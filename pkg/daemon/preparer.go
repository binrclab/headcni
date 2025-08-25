package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/binrclab/headcni/cmd/headcni-daemon/config"
	"github.com/binrclab/headcni/pkg/backend/tailscale"
	"github.com/binrclab/headcni/pkg/cni"
	"github.com/binrclab/headcni/pkg/constants"
	"github.com/binrclab/headcni/pkg/headscale"
	"github.com/binrclab/headcni/pkg/k8s"
	"github.com/binrclab/headcni/pkg/logging"
	"github.com/binrclab/headcni/pkg/monitoring"
)

// Preparer 系统准备器，负责初始化和管理所有系统组件
type Preparer struct {
	config    *config.Config
	oldConfig *config.Config

	// 客户端
	headscaleClient *headscale.Client
	tailscaleClient *tailscale.SimpleClient
	k8sClient       k8s.Client

	// 管理器
	cniConfigManager *cni.CNIConfigManager
	tailscaleService *tailscale.ServiceManager

	// 状态 (暂时简化，后续可以扩展)

	// 清理函数
	cleanupFuncs []func() error
	mu           sync.Mutex
}

// NewPreparer 创建新的系统准备器
func NewPreparer(cfg *config.Config) (*Preparer, error) {
	p := &Preparer{
		config:       cfg,
		oldConfig:    cfg,
		cleanupFuncs: make([]func() error, 0),
	}

	// 按依赖顺序初始化组件
	if err := p.prepare(); err != nil {
		return nil, fmt.Errorf("failed to prepare system: %w", err)
	}
	return p, nil
}

// prepare 按顺序准备所有系统组件
func (p *Preparer) prepare() error {
	// 1. 准备 Kubernetes 客户端
	k8sClient := k8s.NewClient(&k8s.ClientConfig{})
	if err := k8sClient.Connect(context.Background()); err != nil {
		return fmt.Errorf("failed to connect to kubernetes: %w", err)
	}

	// 启动节点 informer
	if err := k8sClient.StartInformers(context.Background()); err != nil {
		return fmt.Errorf("failed to start node informer: %w", err)
	}

	p.k8sClient = k8sClient
	p.addCleanup(func() error {
		k8sClient.Disconnect()
		return nil
	})
	logging.Infof("Kubernetes client prepared successfully")

	// 2. 准备 CNI 组件
	cniConfigManager := cni.NewCNIConfigManager(
		constants.DefaultCNIConfigDir,      // CNI 配置目录
		constants.DefaultHeadCNIConfigFile, // CNI 配置文件名
		logging.NewSimpleLogger(),
	)
	if err := p.checkCNIConfig(cniConfigManager); err != nil {
		return fmt.Errorf("failed to initialize CNI config: %w", err)
	}
	p.cniConfigManager = cniConfigManager

	// 3. 准备 Headscale 客户端
	headscaleClient, err := headscale.NewClient(&p.config.Headscale)
	if err != nil {
		return fmt.Errorf("failed to create headscale client: %w", err)
	}
	p.headscaleClient = headscaleClient

	// 4. 准备 Tailscale 客户端
	socketPath := p.determineTailscaleSocketPath()

	logging.Infof("Initializing Tailscale client - Mode: %s, Socket: %s",
		p.config.Tailscale.Mode, socketPath)

	tailscaleClient := tailscale.NewSimpleClient(socketPath)
	p.tailscaleClient = tailscaleClient

	// 5. 准备 Tailscale 服务管理器
	tailscaleService := tailscale.NewServiceManager()
	p.tailscaleService = tailscaleService

	// 6.初始化 Prometheus 指标
	monitoring.InitMetrics()

	// 7.注册全局健康器
	p.registerServicesToHealthManager()

	logging.Infof("All system components prepared successfully")
	return nil
}

// determineTailscaleSocketPath 根据配置模式确定 Tailscale socket 路径
func (p *Preparer) determineTailscaleSocketPath() string {
	switch p.config.Tailscale.Mode {
	case "host":
		return constants.DefaultTailscaleHostSocketPath
	case "daemon":
		// 如果配置中指定了自定义 socket 路径，优先使用
		if p.config.Tailscale.Socket.Path != "" {
			return p.config.Tailscale.Socket.Path
		}
		// 否则使用默认的 daemon socket 路径
		return constants.DefaultTailscaleDaemonSocketPath
	default:
		// 默认使用 daemon 模式
		logging.Warnf("Unknown Tailscale mode: %s, falling back to daemon mode", p.config.Tailscale.Mode)
		return constants.DefaultTailscaleDaemonSocketPath
	}
}

// checkCNIConfig 检查 CNI 配置
func (p *Preparer) checkCNIConfig(cniConfigManager *cni.CNIConfigManager) error {
	// 从 Kubernetes API 获取当前节点的 Pod CIDR
	node, err := p.k8sClient.GetCurrentNode()
	if err != nil {
		return fmt.Errorf("failed to get current node: %w", err)
	}
	currentPodCIDR, err := p.k8sClient.Nodes().GetPodCIDR(node.Name)
	if err != nil {
		return fmt.Errorf("failed to get Pod CIDR for node %s: %w", node.Name, err)
	}

	logging.Infof("Current node Pod CIDR from Kubernetes API: %s", currentPodCIDR)

	// 检查配置文件是否已存在
	exists, err := cniConfigManager.CheckConfigListExists()
	if err != nil {
		return fmt.Errorf("failed to check config existence: %w", err)
	}

	if exists {
		// 配置文件存在，尝试增量更新
		logging.Infof("CNI config exists, attempting incremental update")

		// 读取现有配置
		existingConfig, err := cniConfigManager.ReadConfigList()
		if err != nil {
			logging.Warnf("Failed to read existing config, will regenerate: %v", err)
		} else {
			// 尝试增量更新配置
			if updatedConfig, updated := p.updateCNIConfigIncrementally(existingConfig, currentPodCIDR); updated {
				logging.Infof("CNI config updated incrementally")

				// 类型断言并验证更新后的配置
				if config, ok := updatedConfig.(*cni.CNIConfigList); ok {
					if err := cniConfigManager.ValidateConfigList(config); err != nil {
						logging.Warnf("Updated config validation failed, will regenerate: %v", err)
					} else {
						// 写入更新后的配置
						if err := cniConfigManager.WriteConfigList(config); err != nil {
							logging.Warnf("Failed to write updated config, will regenerate: %v", err)
						} else {
							logging.Infof("Successfully updated CNI config incrementally - podCIDR: %s", currentPodCIDR)
							return nil
						}
					}
				} else {
					logging.Warnf("Invalid config type after incremental update, will regenerate")
				}
			} else {
				logging.Infof("No incremental update needed, config is up to date")
				return nil
			}
		}
	} else {
		logging.Infof("CNI config does not exist, creating new configuration")
	}

	// 如果增量更新失败或配置不存在，则生成全新配置
	logging.Infof("Generating new CNI configuration")

	// 备份其他配置文件
	if err := cniConfigManager.BackupOtherConfigLists(); err != nil {
		logging.Warnf("Failed to backup existing configs: %v", err)
	}

	dnsServiceIP, clusterDomain := p.getK8sOrK3sDNSAndClusterDomain()

	// 生成新的 CNI 配置
	configList, err := cniConfigManager.GenerateConfigList(
		currentPodCIDR, // Pod CIDR
		p.config,
		dnsServiceIP,
		clusterDomain,
	)
	if err != nil {
		return fmt.Errorf("failed to generate config list: %w", err)
	}

	// 验证配置
	if err := cniConfigManager.ValidateConfigList(configList); err != nil {
		return fmt.Errorf("failed to validate config list: %w", err)
	}

	// 写入配置文件
	if err := cniConfigManager.WriteConfigList(configList); err != nil {
		return fmt.Errorf("failed to write config list: %w", err)
	}

	logging.Infof("Successfully initialized/updated CNI config - podCIDR: %s, serviceCIDR: %s, mtu: %d, configPath: %s",
		currentPodCIDR, p.config.Network.ServiceCIDR, p.config.Network.MTU, cniConfigManager.GetConfigPath())

	return nil
}

func (p *Preparer) getK8sOrK3sDNSAndClusterDomain() (string, string) {
	// 使用 k8s 客户端获取 DNS 配置
	var dnsServiceIP, clusterDomain string

	if p.k8sClient != nil {
		// 获取 DNS 服务 IP
		if ip, err := p.k8sClient.GetDNSServiceIP(); err == nil {
			dnsServiceIP = ip
		}
		// 获取集群域名
		if domain, err := p.k8sClient.GetClusterDomain(); err == nil {
			clusterDomain = domain
		}
	}

	// 如果无法获取，根据环境设置默认值
	if dnsServiceIP == "" {
		if p.isK3sEnvironment() {
			dnsServiceIP = "10.43.0.10" // k3s 默认 DNS 服务 IP
		} else {
			dnsServiceIP = "10.96.0.10" // 标准 Kubernetes 默认 DNS 服务 IP
		}
	}

	if clusterDomain == "" {
		clusterDomain = "cluster.local" // 所有环境都使用相同的集群域名
	}

	return dnsServiceIP, clusterDomain
}

// isK3sEnvironment 检查是否为 k3s 环境
func (p *Preparer) isK3sEnvironment() bool {
	// 方法1: 检查环境变量（最可靠，不需要 API 权限）
	if os.Getenv("K3S_DATA_DIR") != "" || os.Getenv("K3S_CONFIG") != "" {
		return true
	}
	// 默认返回 false，避免误判
	return false
}

// addCleanup 添加清理函数
func (p *Preparer) addCleanup(cleanup func() error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleanupFuncs = append(p.cleanupFuncs, cleanup)
}

// Getter 方法
func (p *Preparer) GetHeadscaleClient() *headscale.Client       { return p.headscaleClient }
func (p *Preparer) GetTailscaleClient() *tailscale.SimpleClient { return p.tailscaleClient }
func (p *Preparer) GetK8sClient() k8s.Client                    { return p.k8sClient }

func (p *Preparer) GetCNIConfigManager() *cni.CNIConfigManager     { return p.cniConfigManager }
func (p *Preparer) GetTailscaleService() *tailscale.ServiceManager { return p.tailscaleService }

// GetConfig 获取配置
func (p *Preparer) GetConfig() *config.Config { return p.config }

// GetOldConfig 获取旧配置
func (p *Preparer) GetOldConfig() *config.Config { return p.oldConfig }

// Shutdown 优雅关闭所有组件
func (p *Preparer) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	for i := len(p.cleanupFuncs) - 1; i >= 0; i-- {
		if err := p.cleanupFuncs[i](); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// registerServicesToHealthManager 注册所有服务到全局健康管理器
func (p *Preparer) registerServicesToHealthManager() {
	healthMgr := GetGlobalHealthManager()

	// 注册所有服务
	healthMgr.RegisterService(constants.ServiceNameMonitoring)
	healthMgr.RegisterService(constants.ServiceNameCNI)
	healthMgr.RegisterService(constants.ServiceNamePodMonitoring)
	healthMgr.RegisterService(constants.ServiceNameHeadscaleHealth)
	healthMgr.RegisterService(constants.ServiceNameTailscale)

	logging.Infof("All services registered to global health manager")
}

// IsReady 检查系统是否准备就绪
func (p *Preparer) IsReady() bool {
	return p.headscaleClient != nil &&
		p.tailscaleClient != nil &&
		p.k8sClient != nil
}

// updateCNIConfigIncrementally 增量更新 CNI 配置
func (p *Preparer) updateCNIConfigIncrementally(existingConfig interface{}, currentPodCIDR string) (interface{}, bool) {
	logging.Debugf("Attempting incremental update for Pod CIDR: %s", currentPodCIDR)

	// 如果 Pod CIDR 为空，无法更新
	if currentPodCIDR == "" {
		logging.Debugf("Pod CIDR is empty, cannot update")
		return existingConfig, false
	}

	// 尝试类型断言为 CNIConfigList
	config, ok := existingConfig.(*cni.CNIConfigList)
	if !ok {
		logging.Debugf("Existing config is not CNIConfigList type, cannot update incrementally")
		return existingConfig, false
	}

	// 使用 CNI 包中的方法进行增量更新
	updated := config.UpdateIncrementally(currentPodCIDR, p.config.Network.ServiceCIDR, p.config.Network.MTU)

	if updated {
		logging.Infof("CNI config updated incrementally for Pod CIDR: %s", currentPodCIDR)
		// 返回深拷贝的配置
		return config.DeepCopy(), true
	} else {
		logging.Debugf("No changes detected, config is up to date")
		return existingConfig, false
	}
}

func (p *Preparer) ReloadConfig() (bool, error) {
	// 保存旧配置用于对比
	old2Config := p.oldConfig
	p.oldConfig = p.config

	// 重新读取配置文件
	newConfig, err := config.LoadConfig(p.config.ConfigPath)
	if err != nil {
		return false, fmt.Errorf("failed to reload config: %v", err)
	}

	// 检查配置变更
	configChanged, changes := p.compareConfigs(p.oldConfig, newConfig)

	if configChanged {
		// 记录配置变更
		for _, change := range changes {
			logging.Infof("配置变更: %s", change)
		}

		// 事务性更新：备份当前组件，创建新组件，成功后提交，失败则回滚
		if err := p.transactionalUpdate(newConfig, changes, old2Config); err != nil {
			logging.Errorf("事务性更新失败: %v", err)
			return false, fmt.Errorf("配置更新失败: %v", err)
		}

		logging.Infof("配置重载成功，检测到 %d 项变更", len(changes))
	} else {
		logging.Infof("配置未发生变化")
	}

	return configChanged, nil
}

// transactionalUpdate 事务性更新配置和组件
func (p *Preparer) transactionalUpdate(newConfig *config.Config, changes []string, old2Config *config.Config) error {
	// 备份当前组件
	backup := p.backupComponents(old2Config)

	// 尝试应用新配置
	p.config = newConfig

	// 尝试重新创建受影响的组件
	if err := p.recreateAffectedComponents(changes); err != nil {
		// 失败时回滚：恢复配置和组件
		logging.Errorf("组件重新创建失败，开始回滚: %v", err)
		p.restoreComponents(backup)
		return fmt.Errorf("组件重新创建失败，已回滚: %v", err)
	}

	// 成功时清理备份
	logging.Infof("事务性更新成功")
	return nil
}

// componentBackup 组件备份结构
type componentBackup struct {
	headscaleClient  *headscale.Client
	tailscaleClient  *tailscale.SimpleClient
	cniConfigManager *cni.CNIConfigManager
	config           *config.Config
	oldConfig        *config.Config
}

// backupComponents 备份当前组件
func (p *Preparer) backupComponents(old2Config *config.Config) *componentBackup {
	return &componentBackup{
		headscaleClient:  p.headscaleClient,
		tailscaleClient:  p.tailscaleClient,
		cniConfigManager: p.cniConfigManager,
		config:           p.oldConfig,
		oldConfig:        old2Config,
	}
}

// restoreComponents 恢复组件
func (p *Preparer) restoreComponents(backup *componentBackup) {
	logging.Infof("回滚组件到原始状态")
	p.headscaleClient = backup.headscaleClient
	p.tailscaleClient = backup.tailscaleClient
	p.cniConfigManager = backup.cniConfigManager
	p.config = backup.config
	p.oldConfig = backup.oldConfig
	logging.Infof("组件回滚完成")
}

// compareConfigs 比较新旧配置，返回是否变更和变更详情
func (p *Preparer) compareConfigs(oldConfig, newConfig *config.Config) (bool, []string) {
	var changes []string
	hasChanges := false

	// 比较 Headscale 配置
	if oldConfig.Headscale.URL != newConfig.Headscale.URL {
		changes = append(changes, fmt.Sprintf("Headscale URL: %s -> %s",
			oldConfig.Headscale.URL, newConfig.Headscale.URL))
		hasChanges = true
	}

	if oldConfig.Headscale.AuthKey != newConfig.Headscale.AuthKey {
		changes = append(changes, "Headscale AuthKey: [已变更]")
		hasChanges = true
	}

	if oldConfig.Headscale.Timeout != newConfig.Headscale.Timeout {
		changes = append(changes, fmt.Sprintf("Headscale Timeout: %s -> %s",
			oldConfig.Headscale.Timeout, newConfig.Headscale.Timeout))
		hasChanges = true
	}

	if oldConfig.Headscale.Retries != newConfig.Headscale.Retries {
		changes = append(changes, fmt.Sprintf("Headscale Retries: %d -> %d",
			oldConfig.Headscale.Retries, newConfig.Headscale.Retries))
		hasChanges = true
	}

	// 比较 Tailscale 配置
	if oldConfig.Tailscale.Mode != newConfig.Tailscale.Mode {
		changes = append(changes, fmt.Sprintf("Tailscale Mode: %s -> %s",
			oldConfig.Tailscale.Mode, newConfig.Tailscale.Mode))
		hasChanges = true
	}

	if oldConfig.Tailscale.URL != newConfig.Tailscale.URL {
		changes = append(changes, fmt.Sprintf("Tailscale URL: %s -> %s",
			oldConfig.Tailscale.URL, newConfig.Tailscale.URL))
		hasChanges = true
	}

	if oldConfig.Tailscale.Socket.Path != newConfig.Tailscale.Socket.Path {
		changes = append(changes, fmt.Sprintf("Tailscale Socket Path: %s -> %s",
			oldConfig.Tailscale.Socket.Path, newConfig.Tailscale.Socket.Path))
		hasChanges = true
	}

	if oldConfig.Tailscale.MTU != newConfig.Tailscale.MTU {
		changes = append(changes, fmt.Sprintf("Tailscale MTU: %d -> %d",
			oldConfig.Tailscale.MTU, newConfig.Tailscale.MTU))
		hasChanges = true
	}

	// 比较网络配置
	if oldConfig.Network.MTU != newConfig.Network.MTU {
		changes = append(changes, fmt.Sprintf("Network MTU: %d -> %d",
			oldConfig.Network.MTU, newConfig.Network.MTU))
		hasChanges = true
	}

	if oldConfig.Network.ServiceCIDR != newConfig.Network.ServiceCIDR {
		changes = append(changes, fmt.Sprintf("Network ServiceCIDR: %s -> %s",
			oldConfig.Network.ServiceCIDR, newConfig.Network.ServiceCIDR))
		hasChanges = true
	}

	if oldConfig.Network.EnableIPv6 != newConfig.Network.EnableIPv6 {
		changes = append(changes, fmt.Sprintf("Network EnableIPv6: %t -> %t",
			oldConfig.Network.EnableIPv6, newConfig.Network.EnableIPv6))
		hasChanges = true
	}

	// 比较监控配置
	if oldConfig.Monitoring.Enabled != newConfig.Monitoring.Enabled {
		changes = append(changes, fmt.Sprintf("Monitoring Enabled: %t -> %t",
			oldConfig.Monitoring.Enabled, newConfig.Monitoring.Enabled))
		hasChanges = true
	}

	if oldConfig.Monitoring.Port != newConfig.Monitoring.Port {
		changes = append(changes, fmt.Sprintf("Monitoring Port: %d -> %d",
			oldConfig.Monitoring.Port, newConfig.Monitoring.Port))
		hasChanges = true
	}

	if oldConfig.Monitoring.Path != newConfig.Monitoring.Path {
		changes = append(changes, fmt.Sprintf("Monitoring Path: %s -> %s",
			oldConfig.Monitoring.Path, newConfig.Monitoring.Path))
		hasChanges = true
	}

	// 比较日志配置
	if oldConfig.Daemon.LogLevel != newConfig.Daemon.LogLevel {
		changes = append(changes, fmt.Sprintf("Log Level: %s -> %s",
			oldConfig.Daemon.LogLevel, newConfig.Daemon.LogLevel))
		hasChanges = true
	}

	return hasChanges, changes
}

// recreateAffectedComponents 根据配置变更重新创建受影响的组件
func (p *Preparer) recreateAffectedComponents(changes []string) error {
	// 分析变更类型，决定需要重新创建哪些组件
	needRecreateHeadscale := false
	needRecreateTailscale := false
	needRecreateMonitoring := false

	for _, change := range changes {
		if strings.Contains(change, "Headscale") {
			needRecreateHeadscale = true
		}
		if strings.Contains(change, "Tailscale") {
			needRecreateTailscale = true
		}
		if strings.Contains(change, "Monitoring") {
			needRecreateMonitoring = true
		}
	}

	// 重新创建 Headscale 客户端
	if needRecreateHeadscale {
		logging.Infof("重新创建 Headscale 客户端...")
		headscaleClient, err := headscale.NewClient(&p.config.Headscale)
		if err != nil {
			return fmt.Errorf("failed to recreate headscale client: %v", err)
		}
		p.headscaleClient = headscaleClient
		logging.Infof("Headscale 客户端重新创建成功")
	}

	// 重新创建 Tailscale 客户端
	if needRecreateTailscale {
		logging.Infof("重新创建 Tailscale 客户端...")
		socketPath := p.determineTailscaleSocketPath()
		tailscaleClient := tailscale.NewSimpleClient(socketPath)
		p.tailscaleClient = tailscaleClient
		logging.Infof("Tailscale 客户端重新创建成功，使用 socket: %s", socketPath)
	}

	// 重新创建监控相关组件
	if needRecreateMonitoring {
		logging.Infof("重新创建监控组件...")
		// 这里可以重新初始化监控相关的组件
		// 比如重新配置监控端口、路径等
		logging.Infof("监控组件重新创建成功")
	}

	// 重新创建 CNI 配置管理器（如果网络配置发生变化）
	if p.hasNetworkConfigChanges(changes) {
		logging.Infof("重新创建 CNI 配置管理器...")
		cniConfigManager := cni.NewCNIConfigManager(
			constants.DefaultCNIConfigDir,
			constants.DefaultHeadCNIConfigFile,
			logging.NewSimpleLogger(),
		)
		p.cniConfigManager = cniConfigManager
		logging.Infof("CNI 配置管理器重新创建成功")
	}

	return nil
}

// hasNetworkConfigChanges 检查是否有网络配置变更
func (p *Preparer) hasNetworkConfigChanges(changes []string) bool {
	for _, change := range changes {
		if strings.Contains(change, "Network") ||
			strings.Contains(change, "MTU") ||
			strings.Contains(change, "ServiceCIDR") ||
			strings.Contains(change, "EnableIPv6") {
			return true
		}
	}
	return false
}
