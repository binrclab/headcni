package cni

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/binrclab/headcni/cmd/daemon/config"
	"github.com/binrclab/headcni/pkg/logging"
	"github.com/binrclab/yamlc"
	"gopkg.in/yaml.v3"
)

// CNIPlugin 是 CNI 插件配置结构
type CNIPlugin struct {
	CNIVersion    string                   `json:"cniVersion,omitempty"`
	Name          string                   `json:"name,omitempty"`
	Type          string                   `json:"type,omitempty"`
	Plugins       []map[string]interface{} `json:"plugins,omitempty"`
	IPAM          map[string]interface{}   `json:"ipam,omitempty"`
	SubnetFile    string                   `json:"subnetFile,omitempty"`
	DataDir       string                   `json:"dataDir,omitempty"`
	Delegate      *Delegate                `json:"delegate,omitempty"`
	RuntimeConfig map[string]interface{}   `json:"runtimeConfig,omitempty"`
}

type Delegate struct {
	Type             string `json:"type,omitempty"`
	Bridge           string `json:"bridge,omitempty"`
	IsDefaultGateway bool   `json:"isDefaultGateway,omitempty"`
	IsGateway        bool   `json:"isGateway,omitempty"`
	HairpinMode      bool   `json:"hairpinMode,omitempty"`
	ForceAddress     bool   `json:"forceAddress,omitempty"`
	IsMasq           bool   `json:"isMasq,omitempty"`
	PromiscMode      bool   `json:"promiscMode,omitempty"`
}

type CniEnv struct {
	NetWork  string    `json:"network,omitempty"      yaml:"network"      comment:"IPv4 network configuration (pod CIDR)"`
	Subnet   string    `json:"subnet,omitempty"       yaml:"subnet"       comment:"IPv4 subnet configuration"`
	IPv6Net  string    `json:"ipv6_network,omitempty" yaml:"ipv6_network" comment:"IPv6 network configuration (pod CIDR)"`
	IPv6Sub  string    `json:"ipv6_subnet,omitempty"  yaml:"ipv6_subnet"  comment:"IPv6 subnet configuration"`
	MTU      int       `json:"mtu,omitempty"          yaml:"mtu"          comment:"MTU configuration"`
	IPMasq   bool      `json:"ipmasq,omitempty"       yaml:"ipmasq"       comment:"IP masquerade configuration"`
	Metadata *Metadata `json:"metadata,omitempty"     yaml:"metadata"     comment:"Metadata information"`
	Routes   []Route   `json:"routes,omitempty"       yaml:"routes"       comment:"Routes configuration"`
	DNS      *DNS      `json:"dns,omitempty"          yaml:"dns"          comment:"DNS configuration"`
	Policies *Policies `json:"policies,omitempty"     yaml:"policies"     comment:"Network policies"`
}

type Metadata struct {
	GeneratedAt string `json:"generated_at,omitempty" yaml:"generated_at" comment:"Generation timestamp"`
	NodeName    string `json:"node_name,omitempty"    yaml:"node_name"    comment:"Node name"`
	ClusterCIDR string `json:"cluster_cidr,omitempty" yaml:"cluster_cidr" comment:"Cluster CIDR"`
	ServiceCIDR string `json:"service_cidr,omitempty" yaml:"service_cidr" comment:"Service CIDR"`
}

type Route struct {
	Dst string `json:"dst,omitempty" yaml:"dst" comment:"Destination CIDR"`
	GW  string `json:"gw,omitempty"  yaml:"gw"  comment:"Gateway IP"`
}

type DNS struct {
	Nameservers []string `json:"nameservers,omitempty" yaml:"nameservers" comment:"DNS nameservers"`
	Search      []string `json:"search,omitempty"      yaml:"search"      comment:"DNS search domains"`
	Options     []string `json:"options,omitempty"     yaml:"options"     comment:"DNS options"`
}

type Policies struct {
	AllowHostAccess     bool `json:"allow_host_access,omitempty"     yaml:"allow_host_access"     comment:"Allow host access"`
	AllowServiceAccess  bool `json:"allow_service_access,omitempty"  yaml:"allow_service_access"  comment:"Allow service access"`
	AllowExternalAccess bool `json:"allow_external_access,omitempty" yaml:"allow_external_access" comment:"Allow external access"`
	EgressAllowed       bool `json:"egress_allowed,omitempty"        yaml:"egress_allowed"        comment:"Allow egress traffic"`
}

// CNIConfigManager 是 CNI 配置管理器
type CNIConfigManager struct {
	configDir  string
	configName string
	cniEnvFile string
	backupDir  string
	logger     logging.Logger
}

// NewCNIConfigManager 创建新的 CNI 配置管理器
func NewCNIConfigManager(configDir, configName, cniEnvFile string, logger logging.Logger) *CNIConfigManager {
	if cniEnvFile == "" {
		cniEnvFile = "/var/lib/headcni/env.yaml"
	}
	return &CNIConfigManager{
		configDir:  configDir,
		configName: configName,
		cniEnvFile: cniEnvFile,
		backupDir:  configDir,
		logger:     logger,
	}
}

// CheckConfigListExists 检查 configlist 是否存在
func (cm *CNIConfigManager) CheckConfigListExists() (bool, error) {
	configPath := filepath.Join(cm.configDir, cm.configName)

	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to check config file: %v", err)
	}

	// 检查文件是否可读
	if _, err := os.ReadFile(configPath); err != nil {
		return false, fmt.Errorf("failed to read config file: %v", err)
	}

	return true, nil
}

// GenerateConfigList 生成 configlist 配置
func (cm *CNIConfigManager) GenerateConfigList(localCIDR string, cfg *config.Config, dnsServiceIP, clusterDomain string) (*CNIPlugin, *CniEnv, error) {
	// ----------------------------cniEnv----------------------------
	// 创建 IPAM 配置，使用 ranges 格式
	var cniEnv = &CniEnv{}

	// 映射 MagicDNS 配置（来自全局配置，若未设置则使用合理默认值）
	// 获取默认 DNS 服务 IP
	defaultDNSIP := dnsServiceIP
	defaultClusterDomain := clusterDomain

	if cfg.DNS.MagicDNS.Enabled {
		cniEnv.DNS = &DNS{
			Nameservers: []string{defaultDNSIP},
			Search:      []string{defaultClusterDomain, fmt.Sprintf("svc.%s", defaultClusterDomain)},
		}

		if len(cfg.DNS.MagicDNS.SearchDomains) > 0 {
			cniEnv.DNS.Search = append(cniEnv.DNS.Search, cfg.DNS.MagicDNS.SearchDomains...)
		}
		if len(cfg.DNS.MagicDNS.Options) > 0 {
			cniEnv.DNS.Options = cfg.DNS.MagicDNS.Options
		}
		if len(cfg.DNS.MagicDNS.Nameservers) > 0 {
			cniEnv.DNS.Nameservers = append(cniEnv.DNS.Nameservers, cfg.DNS.MagicDNS.Nameservers...)
		}
	}
	cniEnv.Policies = &Policies{}
	// 处理 ServiceCIDR 和 LocalCIDR，支持 IPv4 和 IPv6
	var serviceCIDRs []string
	var localCIDRs []string

	if cfg.Network.ServiceCIDR != "" {
		serviceCIDRs = strings.Split(cfg.Network.ServiceCIDR, ",")
	}
	if localCIDR != "" {
		localCIDRs = strings.Split(localCIDR, ",")
	}

	cniEnv.MTU = cfg.Network.MTU
	cniEnv.NetWork = cfg.Network.PodCIDR.Base
	cniEnv.Subnet = localCIDR

	if len(localCIDRs) > 0 {
		cniEnv.Subnet = localCIDRs[0]
		if len(localCIDRs) > 1 {
			cniEnv.IPv6Sub = localCIDRs[1]
		}
	}

	cniEnv.IPMasq = cfg.Network.EnableNetworkPolicy

	// 设置元数据
	cniEnv.Metadata = &Metadata{
		GeneratedAt: time.Now().Format(time.RFC3339),
		NodeName:    os.Getenv("NODE_NAME"), // 使用默认节点名
		ClusterCIDR: cfg.Network.PodCIDR.Base,
		ServiceCIDR: cfg.Network.ServiceCIDR,
	}

	// 设置路由信息
	if len(serviceCIDRs) > 0 {
		cniEnv.Routes = []Route{
			{Dst: serviceCIDRs[0]},
		}
	}

	// ----------------------------cniEnv----------------------------

	// ----------------------------configlist----------------------------

	// 创建 cni 插件配置
	var cniPlugins []map[string]interface{}

	// 将 headcniPlugin 转换为 map[string]interface{}
	headcniPluginBytes, err := json.Marshal(CNIPlugin{
		Type: "headcni",
		Delegate: &Delegate{
			HairpinMode:      true,
			IsDefaultGateway: true,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal headcni plugin: %v", err)
	}

	var headcniPluginMap map[string]interface{}
	if err := json.Unmarshal(headcniPluginBytes, &headcniPluginMap); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal headcni plugin: %v", err)
	}

	// 将 headcniPlugin 添加到插件列表
	cniPlugins = append(cniPlugins, headcniPluginMap)

	// 按优先级排序 CNI 插件
	type pluginWithPriority struct {
		plugin   map[string]interface{}
		priority int
		name     string
	}

	var pluginsWithPriority []pluginWithPriority

	// 处理其他 CNI 插件配置
	for _, plugin := range cfg.CNIPlugins {
		if !plugin.Enabled {
			continue
		}
		// json加载plugin.Config
		// 然后解析为map[string]interface{}
		var pluginConfig map[string]interface{}
		if err := json.Unmarshal([]byte(plugin.Config), &pluginConfig); err != nil {
			logging.Warnf("failed to unmarshal plugin config %s -> %s: %v", plugin.Name, plugin.Config, err)
			continue
		}

		// 将插件和优先级信息保存到临时结构
		pluginsWithPriority = append(pluginsWithPriority, pluginWithPriority{
			plugin:   pluginConfig,
			priority: plugin.Priority,
			name:     plugin.Name,
		})
	}

	// 按优先级排序（数字越小优先级越高）
	sort.Slice(pluginsWithPriority, func(i, j int) bool {
		return pluginsWithPriority[i].priority < pluginsWithPriority[j].priority
	})

	// 按排序后的顺序添加到插件列表
	for _, pwp := range pluginsWithPriority {
		cniPlugins = append(cniPlugins, pwp.plugin)
		logging.Debugf("Added plugin %s with priority %d", pwp.name, pwp.priority)
	}

	// 创建完整的配置列表
	configList := &CNIPlugin{
		CNIVersion: "1.0.0",
		Name:       "cbr0",
		Plugins:    cniPlugins,
	}

	return configList, cniEnv, nil
}

// WriteConfigList 写入 configlist 到文件
func (cm *CNIConfigManager) WriteConfigList(configList *CNIPlugin) error {
	// 确保配置目录存在
	if err := os.MkdirAll(cm.configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	// 备份现有配置
	if err := cm.backupExistingConfigs(); err != nil {
		logging.Warnf("Failed to backup existing configs: %v", err)
	}

	// 序列化配置
	configData, err := json.MarshalIndent(configList, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	// 写入配置文件
	configPath := filepath.Join(cm.configDir, cm.configName)
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	logging.Infof("Successfully wrote CNI config: %s", configPath)
	return nil
}

// WriteCniEnv 写入 cniEnv 配置（使用 yamlc 库）
func (cm *CNIConfigManager) WriteCniEnv(cniEnv *CniEnv) error {
	// 确保配置目录存在
	if err := os.MkdirAll(filepath.Dir(cm.cniEnvFile), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	// 使用 yamlc 库生成 YAML 文件，使用 StyleInline 样式以获得更好的格式
	yamlData, err := yamlc.Gen(cniEnv, yamlc.WithStyle(yamlc.StyleInline))
	if err != nil {
		return fmt.Errorf("failed to generate YAML with yamlc: %v", err)
	}

	// 写入配置文件
	if err := os.WriteFile(cm.cniEnvFile, yamlData, 0644); err != nil {
		return fmt.Errorf("failed to write cniEnv file: %v", err)
	}

	logging.Infof("Successfully wrote CNI env with yamlc: %s", cm.cniEnvFile)
	return nil
}

// WriteConfigListAndEnv 同时写入 CNI 配置和 CNI 环境
func (cm *CNIConfigManager) WriteConfigListAndEnv(configList *CNIPlugin, cniEnv *CniEnv) error {
	// 写入 CNI 配置
	if err := cm.WriteConfigList(configList); err != nil {
		return fmt.Errorf("failed to write CNI config: %v", err)
	}

	// 写入 CNI 环境
	if err := cm.WriteCniEnv(cniEnv); err != nil {
		return fmt.Errorf("failed to write CNI env: %v", err)
	}

	logging.Infof("Successfully wrote both CNI config and CNI env")
	return nil
}

// ReadCniEnv 读取 cniEnv 配置

func (cm *CNIConfigManager) ReadCniEnv() (*CniEnv, error) {
	// 读取配置文件
	cniEnvBytes, err := os.ReadFile(cm.cniEnvFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read cniEnv file: %v", err)
	}

	var cniEnv CniEnv
	if err := yaml.Unmarshal(cniEnvBytes, &cniEnv); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cniEnv: %v", err)
	}

	return &cniEnv, nil
}

// ReadConfigList 读取 configlist 配置
func (cm *CNIConfigManager) ReadConfigList() (*CNIPlugin, error) {
	configPath := filepath.Join(cm.configDir, cm.configName)

	// 读取配置文件
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	// 解析配置
	var configList CNIPlugin
	if err := json.Unmarshal(configData, &configList); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	return &configList, nil
}

// BackupOtherConfigLists 备份其他的 configlist
func (cm *CNIConfigManager) BackupOtherConfigLists() error {
	// 确保备份目录存在
	if err := os.MkdirAll(cm.backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %v", err)
	}

	// 读取配置目录中的所有文件
	files, err := os.ReadDir(cm.configDir)
	if err != nil {
		return fmt.Errorf("failed to read config directory: %v", err)
	}

	backupCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()
		// 跳过当前配置文件和非 .conflist 文件
		if fileName == cm.configName || !strings.HasSuffix(fileName, ".conflist") {
			continue
		}

		// 备份文件
		if err := cm.backupFile(fileName); err != nil {
			logging.Errorf("Failed to backup file %s: %v", fileName, err)
			continue
		}

		backupCount++
	}

	logging.Infof("Backed up other config lists: %d", backupCount)
	return nil
}

// backupExistingConfigs 备份其他插件配置 比如 configlist config 等 cni 插件配置
func (cm *CNIConfigManager) backupExistingConfigs() error {
	// 定义需要备份的文件扩展名（CNI 标准格式）
	backupExtensions := []string{
		".conflist", // CNI 配置列表文件
		".conf",     // 单个 CNI 配置文件
		".json",     // JSON 格式配置文件
		".yaml",     // YAML 格式配置文件
		".yml",      // YAML 格式配置文件（短后缀）
	}

	// 遍历 configDir 下的所有文件
	files, err := os.ReadDir(cm.configDir)
	if err != nil {
		return fmt.Errorf("failed to read config directory: %v", err)
	}

	var backupCount int
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()

		// 检查文件扩展名是否需要备份
		shouldBackup := false
		for _, ext := range backupExtensions {
			if strings.HasSuffix(fileName, ext) {
				shouldBackup = true
				break
			}
		}

		if shouldBackup {
			if err := cm.backupFile(fileName); err != nil {
				return fmt.Errorf("failed to backup file %s: %v", fileName, err)
			}
			backupCount++
		}
	}

	if backupCount > 0 {
		logging.Infof("Backed up %d existing config files", backupCount)
	} else {
		logging.Debugf("No existing config files found to backup")
	}

	return nil
}

// backupFile 备份单个文件
func (cm *CNIConfigManager) backupFile(fileName string) error {
	sourcePath := filepath.Join(cm.configDir, fileName)

	// 检查源文件是否存在
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return fmt.Errorf("source file does not exist: %s", sourcePath)
	}

	// 生成备份文件名
	backupFileName := fmt.Sprintf("%s.headcni_bak", fileName)
	backupPath := filepath.Join(cm.backupDir, backupFileName)

	// 确保备份目录存在
	if err := os.MkdirAll(cm.backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %v", err)
	}

	// 读取源文件
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read source file %s: %v", sourcePath, err)
	}

	// 写入备份文件
	if err := os.WriteFile(backupPath, sourceData, 0644); err != nil {
		return fmt.Errorf("failed to write backup file %s: %v", backupPath, err)
	}

	// 删除源文件
	if err := os.Remove(sourcePath); err != nil {
		// 如果删除失败，尝试删除备份文件以保持一致性
		_ = os.Remove(backupPath)
		return fmt.Errorf("failed to remove source file %s: %v", sourcePath, err)
	}

	logging.Infof("Backed up config file: %s -> %s", fileName, backupFileName)
	return nil
}

// restoreBackup 恢复备份文件
func (cm *CNIConfigManager) restoreBackup(backupFileName string) error {
	backupPath := filepath.Join(cm.backupDir, backupFileName)

	// 检查备份文件是否存在
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file does not exist: %s", backupPath)
	}

	// 从备份文件名中提取原始文件名
	// 格式: original_name.headcni_bak
	parts := strings.Split(backupFileName, ".headcni_bak")
	if len(parts) != 2 {
		return fmt.Errorf("invalid backup file name format: %s", backupFileName)
	}
	originalFileName := parts[0]

	// 恢复原始扩展名
	originalExtensions := []string{".conflist", ".conf", ".json", ".yaml", ".yml"}
	hasExtension := false
	for _, ext := range originalExtensions {
		if strings.HasSuffix(originalFileName, ext) {
			hasExtension = true
			break
		}
	}
	// 如果没有找到扩展名，添加默认的 .conf
	if !hasExtension {
		originalFileName += ".conf"
	}

	restorePath := filepath.Join(cm.configDir, originalFileName)

	// 读取备份文件
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup file %s: %v", backupPath, err)
	}

	// 写入恢复文件
	if err := os.WriteFile(restorePath, backupData, 0644); err != nil {
		return fmt.Errorf("failed to write restore file %s: %v", restorePath, err)
	}

	// 删除备份文件
	if err := os.Remove(backupPath); err != nil {
		logging.Warnf("Failed to remove backup file %s: %v", backupPath, err)
	}

	logging.Infof("Restored config file: %s -> %s", backupFileName, originalFileName)
	return nil
}

// listBackups 列出所有备份文件
func (cm *CNIConfigManager) listBackups() ([]string, error) {
	files, err := os.ReadDir(cm.backupDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %v", err)
	}

	var backups []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if strings.Contains(file.Name(), ".headcni_bak") {
			backups = append(backups, file.Name())
		}
	}

	return backups, nil
}

// cleanupOldBackups 清理旧的备份文件（保留最近N个）
func (cm *CNIConfigManager) cleanupOldBackups(keepCount int) error {
	backups, err := cm.listBackups()
	if err != nil {
		return fmt.Errorf("failed to list backups: %v", err)
	}

	if len(backups) <= keepCount {
		return nil // 不需要清理
	}

	// 按修改时间排序（最新的在前）
	type backupInfo struct {
		name    string
		modTime int64
	}

	var backupInfos []backupInfo
	for _, backup := range backups {
		backupPath := filepath.Join(cm.backupDir, backup)
		info, err := os.Stat(backupPath)
		if err != nil {
			logging.Warnf("Failed to get backup file info %s: %v", backup, err)
			continue
		}
		backupInfos = append(backupInfos, backupInfo{
			name:    backup,
			modTime: info.ModTime().Unix(),
		})
	}

	// 按修改时间排序
	sort.Slice(backupInfos, func(i, j int) bool {
		return backupInfos[i].modTime > backupInfos[j].modTime
	})

	// 删除旧的备份文件
	for i := keepCount; i < len(backupInfos); i++ {
		backupPath := filepath.Join(cm.backupDir, backupInfos[i].name)
		if err := os.Remove(backupPath); err != nil {
			logging.Warnf("Failed to remove old backup %s: %v", backupInfos[i].name, err)
		} else {
			logging.Debugf("Removed old backup: %s", backupInfos[i].name)
		}
	}

	logging.Infof("Cleaned up %d old backup files", len(backupInfos)-keepCount)
	return nil
}

// GetConfigPath 获取配置文件路径
func (cm *CNIConfigManager) GetConfigPath() string {
	return filepath.Join(cm.configDir, cm.configName)
}

// GetBackupDir 获取备份目录路径
func (cm *CNIConfigManager) GetBackupDir() string {
	return cm.backupDir
}
