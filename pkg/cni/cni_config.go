package cni

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/binrclab/headcni/cmd/daemon/config"
	"github.com/binrclab/headcni/pkg/logging"
	"gopkg.in/yaml.v3"
)

// CNI环境配置模板
const cniEnvTemplate = `# HeadCNI Environment Configuration
# Generated at: {{.GeneratedAt}}
# This file replaces the old subnet.env format with a more structured YAML format

{{if .Network}}# IPv4 network configuration ( Service CIDR)
network: "{{.Network}}"
{{end}}{{if .Subnet}}# IPv4 subnet configuration
subnet: "{{.Subnet}}"
{{end}}{{if .IPv6Net}}# IPv6 network configuration ( Service CIDR)
ipv6_network: "{{.IPv6Net}}"
{{end}}{{if .IPv6Sub}}# IPv6 subnet configuration
ipv6_subnet: "{{.IPv6Sub}}"
{{end}}{{if .MTU}}# MTU configuration (can be integer or string)
mtu: {{.MTU}}
{{end}}{{if .IPMasq}}# IP masquerade configuration (can be boolean or string)
ipmasq: {{.IPMasq}}
{{end}}{{if and .DNS .DNS.Nameservers}}# DNS configuration
dns:
  nameservers: {{range .DNS.Nameservers}}
    - {{.}}{{end}}{{if and .DNS.Search .DNS.Search}}  search: {{range .DNS.Search}}
    - {{.}}{{end}}{{end}}{{if and .DNS.Options .DNS.Options}}  options: {{range .DNS.Options}}
    - {{.}}{{end}}{{end}}
{{end}}{{if and .Routes .Routes}}# Routes configuration
routes: {{range .Routes}}
  - dst: "{{.Dst}}"{{if .GW}}
    gw: "{{.GW}}"{{end}}{{end}}
{{end}}{{if and .Metadata .Metadata.NodeName}}# Metadata
metadata:
  generated_at: "{{.Metadata.GeneratedAt}}"{{if .Metadata.NodeName}}  node_name: "{{.Metadata.NodeName}}"{{end}}{{if .Metadata.ClusterCIDR}}  cluster_cidr: "{{.Metadata.ClusterCIDR}}"{{end}}{{if .Metadata.ServiceCIDR}}  service_cidr: "{{.Metadata.ServiceCIDR}}"{{end}}
{{end}}`

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
	NetWork  string    `json:"network,omitempty"`
	Subnet   string    `json:"subnet,omitempty"`
	IPv6Net  string    `json:"ipv6_network,omitempty"`
	IPv6Sub  string    `json:"ipv6_subnet,omitempty"`
	MTU      int       `json:"mtu,omitempty"`
	IPMasq   bool      `json:"ipmasq,omitempty"`
	Metadata *Metadata `json:"metadata,omitempty"`
	Routes   []Route   `json:"routes,omitempty"`
	DNS      *DNS      `json:"dns,omitempty"`
	Policies *Policies `json:"policies,omitempty"`
}

type Metadata struct {
	GeneratedAt string `json:"generated_at,omitempty"`
	NodeName    string `json:"node_name,omitempty"`
	ClusterCIDR string `json:"cluster_cidr,omitempty"`
	ServiceCIDR string `json:"service_cidr,omitempty"`
}

type Route struct {
	Dst string `json:"dst,omitempty"`
	GW  string `json:"gw,omitempty"`
}

type DNS struct {
	Nameservers []string `json:"nameservers,omitempty"`
	Search      []string `json:"search,omitempty"`
	Options     []string `json:"options,omitempty"`
}

type Policies struct {
	AllowHostAccess     bool `json:"allow_host_access,omitempty"`
	AllowServiceAccess  bool `json:"allow_service_access,omitempty"`
	AllowExternalAccess bool `json:"allow_external_access,omitempty"`
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
	cniEnv.NetWork = cfg.Network.ServiceCIDR
	cniEnv.Subnet = localCIDR

	// 设置 IPv4 和 IPv6 网络信息
	if len(serviceCIDRs) > 0 {
		cniEnv.NetWork = serviceCIDRs[0]
		if len(serviceCIDRs) > 1 {
			cniEnv.IPv6Net = serviceCIDRs[1]
		}
	}

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
		ClusterCIDR: localCIDR,
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
		Type:       "headcni",
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

// WriteCniEnv 写入 cniEnv 配置（使用模板）
func (cm *CNIConfigManager) WriteCniEnv(cniEnv *CniEnv) error {
	// 确保配置目录存在
	if err := os.MkdirAll(filepath.Dir(cm.cniEnvFile), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	// 不再需要序列化，直接使用模板生成

	// 解析模板
	tmpl, err := template.New("cniEnv").Parse(cniEnvTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %v", err)
	}

	// 准备模板数据，确保所有字段都有默认值
	templateData := map[string]interface{}{
		"GeneratedAt": time.Now().Format(time.RFC3339),
		"Network":     getStringOrDefault(cniEnv.NetWork, ""),
		"Subnet":      getStringOrDefault(cniEnv.Subnet, ""),
		"MTU":         getIntOrDefault(cniEnv.MTU, 1500),
		"IPMasq":      getBoolOrDefault(cniEnv.IPMasq, false),
		"IPv6Net":     getStringOrDefault(cniEnv.IPv6Net, ""),
		"IPv6Sub":     getStringOrDefault(cniEnv.IPv6Sub, ""),
		"Metadata":    getMetadataOrDefault(cniEnv.Metadata),
		"DNS":         getDNSOrDefault(cniEnv.DNS),
		"Routes":      getRoutesOrDefault(cniEnv.Routes),
	}

	// 执行模板
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, templateData)
	if err != nil {
		return fmt.Errorf("failed to execute template: %v", err)
	}

	// 写入配置文件
	if err := os.WriteFile(cm.cniEnvFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write cniEnv file: %v", err)
	}

	logging.Infof("Successfully wrote CNI env with template: %s", cm.cniEnvFile)
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

	// 定义需要备份的文件名前缀模式
	backupPrefixes := []string{
		"10-", // 标准 CNI 配置前缀
		"20-", // 标准 CNI 配置前缀
		"30-", // 标准 CNI 配置前缀
		"40-", // 标准 CNI 配置前缀
		"50-", // 标准 CNI 配置前缀
		"60-", // 标准 CNI 配置前缀
		"70-", // 标准 CNI 配置前缀
		"80-", // 标准 CNI 配置前缀
		"90-", // 标准 CNI 配置前缀
		"99-", // 标准 CNI 配置前缀
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

		// 检查文件前缀是否需要备份
		if !shouldBackup {
			for _, prefix := range backupPrefixes {
				if strings.HasPrefix(fileName, prefix) {
					shouldBackup = true
					break
				}
			}
		}

		// 检查是否包含 CNI 相关关键词
		if !shouldBackup {
			cniKeywords := []string{"cni", "network", "bridge", "flannel", "calico", "weave", "canal"}
			lowerFileName := strings.ToLower(fileName)
			for _, keyword := range cniKeywords {
				if strings.Contains(lowerFileName, keyword) {
					shouldBackup = true
					break
				}
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

// 移除深拷贝函数，直接覆盖更简单

// 移除复杂的增量更新函数，直接覆盖更简单

// 辅助函数：安全获取字符串值，空值时返回空字符串（模板中不显示）
func getStringOrDefault(value, defaultValue string) string {
	if value == "" {
		return ""
	}
	return value
}

// 辅助函数：安全获取整数值，0值时返回0（模板中不显示）
func getIntOrDefault(value, defaultValue int) int {
	if value == 0 {
		return 0
	}
	return value
}

// 辅助函数：安全获取布尔值，false值时返回false（模板中不显示）
func getBoolOrDefault(value, defaultValue bool) bool {
	return value
}

// 辅助函数：安全获取Metadata，nil时返回nil（模板中不显示）
func getMetadataOrDefault(metadata *Metadata) *Metadata {
	if metadata == nil {
		return nil
	}
	// 如果metadata存在但字段为空，也返回nil
	if metadata.NodeName == "" && metadata.ClusterCIDR == "" && metadata.ServiceCIDR == "" {
		return nil
	}
	return metadata
}

// 辅助函数：安全获取DNS配置，nil时返回nil（模板中不显示）
func getDNSOrDefault(dns *DNS) *DNS {
	if dns == nil {
		return nil
	}
	// 如果DNS存在但所有字段都为空，也返回nil
	if len(dns.Nameservers) == 0 && len(dns.Search) == 0 && len(dns.Options) == 0 {
		return nil
	}
	return dns
}

// 辅助函数：安全获取路由配置，nil或空时返回nil（模板中不显示）
func getRoutesOrDefault(routes []Route) []Route {
	if routes == nil || len(routes) == 0 {
		return nil
	}
	return routes
}
