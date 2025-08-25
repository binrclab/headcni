package cni

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/binrclab/headcni/cmd/headcni-daemon/config"
	"github.com/binrclab/headcni/pkg/logging"
)

// CNIConfigList 是 CNI 配置列表结构
type CNIConfigList struct {
	CNIVersion   string      `json:"cniVersion"`
	Name         string      `json:"name"`
	Plugins      []CNIPlugin `json:"plugins"`
	DisableCheck bool        `json:"disableCheck,omitempty"`
}

// CNIPlugin 是 CNI 插件配置结构
type CNIPlugin struct {
	Type                string                 `json:"type"`
	Name                string                 `json:"name,omitempty"`
	CNIVersion          string                 `json:"cniVersion,omitempty"`
	IPAM                *IPAMConfig            `json:"ipam,omitempty"`
	Capabilities        map[string]bool        `json:"capabilities,omitempty"`
	DNS                 *DNSConfig             `json:"dns,omitempty"`
	MTU                 int                    `json:"mtu,omitempty"`
	Args                map[string]interface{} `json:"args,omitempty"`
	MagicDNS            *MagicDNSConfig        `json:"magic_dns,omitempty"`
	PodCIDR             string                 `json:"pod_cidr,omitempty"`
	ServiceCIDR         string                 `json:"service_cidr,omitempty"`
	EnableIPv6          bool                   `json:"enable_ipv6,omitempty"`
	EnableNetworkPolicy bool                   `json:"enable_network_policy,omitempty"`
	TailscaleNic        string                 `json:"tailscale_nic,omitempty"`
}

// IPAMConfig 是 IPAM 配置结构
type IPAMConfig struct {
	Type               string                `json:"type"`
	Subnet             string                `json:"subnet,omitempty"`
	Gateway            string                `json:"gateway,omitempty"`
	Routes             []Route               `json:"routes,omitempty"`
	DataDir            string                `json:"dataDir,omitempty"`
	ResolvConf         string                `json:"resolvConf,omitempty"`
	Ranges             [][]map[string]string `json:"ranges,omitempty"`
	AllocationStrategy string                `json:"allocation_strategy,omitempty"`
}

// Route 是路由配置结构
type Route struct {
	Dst string `json:"dst"`
	GW  string `json:"gw,omitempty"`
}

// DNSConfig 是 DNS 配置结构
type DNSConfig struct {
	Nameservers []string `json:"nameservers,omitempty"`
	Search      []string `json:"search,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// MagicDNSConfig 是 MagicDNS 配置结构
type MagicDNSConfig struct {
	Enable        bool     `json:"enable"`
	BaseDomain    string   `json:"base_domain,omitempty"`
	Nameservers   []string `json:"nameservers,omitempty"`
	SearchDomains []string `json:"search_domains,omitempty"`
}

// CNIConfigManager 是 CNI 配置管理器
type CNIConfigManager struct {
	configDir  string
	configName string
	backupDir  string
	logger     logging.Logger
}

// NewCNIConfigManager 创建新的 CNI 配置管理器
func NewCNIConfigManager(configDir, configName string, logger logging.Logger) *CNIConfigManager {
	return &CNIConfigManager{
		configDir:  configDir,
		configName: configName,
		backupDir:  filepath.Join(configDir, "backup"),
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
func (cm *CNIConfigManager) GenerateConfigList(localCIDR string, cfg *config.Config, dnsServiceIP, clusterDomain string) (*CNIConfigList, error) {
	// 创建 IPAM 配置，使用 ranges 格式
	var ipamConfig *IPAMConfig
	if cfg.IPAM.Type == "host-local" {
		ipamConfig = &IPAMConfig{
			Type: "host-local",
			Ranges: [][]map[string]string{
				{
					{"subnet": localCIDR},
				},
			},
			DataDir:            "/var/lib/headcni/networks/headcni",
			AllocationStrategy: cfg.IPAM.Strategy,
		}
	} else {
		ipamConfig = &IPAMConfig{
			Type: "headcni-ipam",
			Ranges: [][]map[string]string{
				{
					{"subnet": localCIDR},
				},
			},
			DataDir:            "/var/lib/headcni/networks/headcni",
			AllocationStrategy: cfg.IPAM.Strategy,
		}
	}

	// 映射 MagicDNS 配置（来自全局配置，若未设置则使用合理默认值）
	// 获取默认 DNS 服务 IP
	defaultDNSIP := dnsServiceIP
	defaultClusterDomain := clusterDomain

	magicDNSCfg := &MagicDNSConfig{
		Enable:        cfg.DNS.MagicDNS.Enabled,
		BaseDomain:    defaultClusterDomain,
		Nameservers:   []string{defaultDNSIP},
		SearchDomains: []string{defaultClusterDomain, fmt.Sprintf("svc.%s", defaultClusterDomain)},
	}
	if len(cfg.DNS.MagicDNS.Nameservers) > 0 {
		magicDNSCfg.Nameservers = append(magicDNSCfg.Nameservers, cfg.DNS.MagicDNS.Nameservers...)
	}
	if len(cfg.DNS.MagicDNS.SearchDomains) > 0 {
		magicDNSCfg.SearchDomains = append(magicDNSCfg.SearchDomains, cfg.DNS.MagicDNS.SearchDomains...)
	}

	interfaceName := cfg.Tailscale.InterfaceName
	if interfaceName == "" {
		interfaceName = "headcni01"
	}

	// 创建 HeadCNI 插件配置
	headcniPlugin := CNIPlugin{
		Type:                "headcni",
		IPAM:                ipamConfig,
		MTU:                 cfg.Network.MTU,
		MagicDNS:            magicDNSCfg,
		ServiceCIDR:         cfg.Network.ServiceCIDR,
		EnableIPv6:          cfg.Network.EnableIPv6,
		EnableNetworkPolicy: cfg.Network.EnableNetworkPolicy,
		TailscaleNic:        interfaceName, // 启用 Tailscale NIC
	}

	// 创建 portmap 插件配置
	portmapPlugin := CNIPlugin{
		Type: "portmap",
		Capabilities: map[string]bool{
			"portMappings": true,
		},
		Args: map[string]interface{}{
			"snat": true,
		},
	}

	// 创建 bandwidth 插件配置
	bandwidthPlugin := CNIPlugin{
		Type: "bandwidth",
		Capabilities: map[string]bool{
			"bandwidth": true,
		},
	}

	// 创建完整的配置列表
	configList := &CNIConfigList{
		CNIVersion: "1.0.0",
		Name:       "headcni",
		Plugins:    []CNIPlugin{headcniPlugin, portmapPlugin, bandwidthPlugin},
	}

	return configList, nil
}

// WriteConfigList 写入 configlist 到文件
func (cm *CNIConfigManager) WriteConfigList(configList *CNIConfigList) error {
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

// ReadConfigList 读取 configlist 配置
func (cm *CNIConfigManager) ReadConfigList() (*CNIConfigList, error) {
	configPath := filepath.Join(cm.configDir, cm.configName)

	// 读取配置文件
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	// 解析配置
	var configList CNIConfigList
	if err := json.Unmarshal(configData, &configList); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	return &configList, nil
}

// UpdateConfigList 更新 configlist 配置
func (cm *CNIConfigManager) UpdateConfigList(updates map[string]interface{}) error {
	// 读取现有配置
	configList, err := cm.ReadConfigList()
	if err != nil {
		return fmt.Errorf("failed to read existing config: %v", err)
	}

	// 应用更新
	if err := cm.applyUpdates(configList, updates); err != nil {
		return fmt.Errorf("failed to apply updates: %v", err)
	}

	// 写入更新后的配置
	if err := cm.WriteConfigList(configList); err != nil {
		return fmt.Errorf("failed to write updated config: %v", err)
	}

	logging.Infof("Successfully updated CNI config")
	return nil
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

// validateCNIConfigFile 验证 CNI 配置文件的有效性
func (cm *CNIConfigManager) validateCNIConfigFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	// 尝试解析为 JSON
	var config interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("invalid JSON format: %v", err)
	}

	// 检查是否为 CNI 配置列表格式
	if configMap, ok := config.(map[string]interface{}); ok {
		// 检查必需字段
		if _, hasCNIVersion := configMap["cniVersion"]; !hasCNIVersion {
			return fmt.Errorf("missing required field: cniVersion")
		}
		if _, hasName := configMap["name"]; !hasName {
			return fmt.Errorf("missing required field: name")
		}

		// 检查是否为配置列表格式
		if plugins, hasPlugins := configMap["plugins"]; hasPlugins {
			if pluginList, ok := plugins.([]interface{}); ok {
				if len(pluginList) == 0 {
					return fmt.Errorf("plugins array cannot be empty")
				}
				// 验证每个插件
				for i, plugin := range pluginList {
					if pluginMap, ok := plugin.(map[string]interface{}); ok {
						if _, hasType := pluginMap["type"]; !hasType {
							return fmt.Errorf("plugin %d missing required field: type", i)
						}
					} else {
						return fmt.Errorf("plugin %d is not a valid object", i)
					}
				}
			} else {
				return fmt.Errorf("plugins field must be an array")
			}
		} else {
			// 单个插件配置
			if _, hasType := configMap["type"]; !hasType {
				return fmt.Errorf("missing required field: type")
			}
		}
	} else {
		return fmt.Errorf("config must be a JSON object")
	}

	return nil
}

// validateBackupFile 验证备份文件的有效性
func (cm *CNIConfigManager) validateBackupFile(backupFileName string) error {
	backupPath := filepath.Join(cm.backupDir, backupFileName)

	// 检查备份文件是否存在
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file does not exist: %s", backupPath)
	}

	// 验证文件格式
	return cm.validateCNIConfigFile(backupPath)
}

// getBackupFileInfo 获取备份文件信息
func (cm *CNIConfigManager) getBackupFileInfo(backupFileName string) (map[string]interface{}, error) {
	backupPath := filepath.Join(cm.backupDir, backupFileName)

	data, err := os.ReadFile(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup file: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse backup file: %v", err)
	}

	return config, nil
}

// applyUpdates 应用配置更新
func (cm *CNIConfigManager) applyUpdates(configList *CNIConfigList, updates map[string]interface{}) error {
	for key, value := range updates {
		switch key {
		case "cniVersion":
			if version, ok := value.(string); ok {
				configList.CNIVersion = version
			}
		case "name":
			if name, ok := value.(string); ok {
				configList.Name = name
			}
		case "disableCheck":
			if disable, ok := value.(bool); ok {
				configList.DisableCheck = disable
			}
		case "mtu":
			if mtu, ok := value.(int); ok {
				for i := range configList.Plugins {
					configList.Plugins[i].MTU = mtu
				}
			}
		case "subnet":
			if subnet, ok := value.(string); ok {
				for i := range configList.Plugins {
					if configList.Plugins[i].IPAM != nil {
						configList.Plugins[i].IPAM.Subnet = subnet
					}
				}
			}
		default:
			logging.Warnf("Unknown config update key: %s", key)
		}
	}

	return nil
}

// ValidateConfigList 验证 configlist 配置
func (cm *CNIConfigManager) ValidateConfigList(configList *CNIConfigList) error {
	if configList.CNIVersion == "" {
		return fmt.Errorf("cniVersion is required")
	}

	if configList.Name == "" {
		return fmt.Errorf("name is required")
	}

	if len(configList.Plugins) == 0 {
		return fmt.Errorf("at least one plugin is required")
	}

	for i, plugin := range configList.Plugins {
		if plugin.Type == "" {
			return fmt.Errorf("plugin %d: type is required", i)
		}
	}

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

// DeepCopyConfig 深拷贝 CNI 配置
func (c *CNIConfigList) DeepCopy() *CNIConfigList {
	if c == nil {
		return nil
	}

	// 创建新的配置对象
	copied := &CNIConfigList{
		CNIVersion:   c.CNIVersion,
		Name:         c.Name,
		DisableCheck: c.DisableCheck,
	}

	// 深拷贝插件配置
	if c.Plugins != nil {
		copied.Plugins = make([]CNIPlugin, len(c.Plugins))
		for i, plugin := range c.Plugins {
			copied.Plugins[i] = CNIPlugin{
				Type:                plugin.Type,
				Name:                plugin.Name,
				CNIVersion:          plugin.CNIVersion,
				MTU:                 plugin.MTU,
				PodCIDR:             plugin.PodCIDR,
				ServiceCIDR:         plugin.ServiceCIDR,
				EnableIPv6:          plugin.EnableIPv6,
				EnableNetworkPolicy: plugin.EnableNetworkPolicy,
				TailscaleNic:        plugin.TailscaleNic,
			}

			// 深拷贝 IPAM 配置
			if plugin.IPAM != nil {
				copied.Plugins[i].IPAM = &IPAMConfig{
					Type:               plugin.IPAM.Type,
					Subnet:             plugin.IPAM.Subnet,
					Gateway:            plugin.IPAM.Gateway,
					DataDir:            plugin.IPAM.DataDir,
					ResolvConf:         plugin.IPAM.ResolvConf,
					AllocationStrategy: plugin.IPAM.AllocationStrategy,
				}

				// 深拷贝 IPAM Routes
				if plugin.IPAM.Routes != nil {
					copied.Plugins[i].IPAM.Routes = make([]Route, len(plugin.IPAM.Routes))
					for j, route := range plugin.IPAM.Routes {
						copied.Plugins[i].IPAM.Routes[j] = Route{
							Dst: route.Dst,
							GW:  route.GW,
						}
					}
				}

				// 深拷贝 IPAM Ranges
				if plugin.IPAM.Ranges != nil {
					copied.Plugins[i].IPAM.Ranges = make([][]map[string]string, len(plugin.IPAM.Ranges))
					for j, rangeGroup := range plugin.IPAM.Ranges {
						copied.Plugins[i].IPAM.Ranges[j] = make([]map[string]string, len(rangeGroup))
						for k, rangeItem := range rangeGroup {
							copied.Plugins[i].IPAM.Ranges[j][k] = make(map[string]string)
							for key, value := range rangeItem {
								copied.Plugins[i].IPAM.Ranges[j][k][key] = value
							}
						}
					}
				}
			}

			// 深拷贝 Capabilities
			if plugin.Capabilities != nil {
				copied.Plugins[i].Capabilities = make(map[string]bool)
				for key, value := range plugin.Capabilities {
					copied.Plugins[i].Capabilities[key] = value
				}
			}

			// 深拷贝 Args
			if plugin.Args != nil {
				copied.Plugins[i].Args = make(map[string]interface{})
				for key, value := range plugin.Args {
					copied.Plugins[i].Args[key] = value
				}
			}
		}
	}

	return copied
}

// UpdateConfigIncrementally 增量更新 CNI 配置
func (c *CNIConfigList) UpdateIncrementally(currentPodCIDR, serviceCIDR string, mtu int) bool {
	if c == nil || currentPodCIDR == "" {
		return false
	}

	updated := false

	// 检查并更新插件配置
	for i, plugin := range c.Plugins {
		// 1. 检查并更新 Pod CIDR
		if plugin.PodCIDR != currentPodCIDR {
			c.Plugins[i].PodCIDR = currentPodCIDR
			updated = true
		}

		// 2. 检查并更新 Service CIDR
		if plugin.ServiceCIDR != serviceCIDR {
			c.Plugins[i].ServiceCIDR = serviceCIDR
			updated = true
		}

		// 3. 检查并更新 MTU
		if plugin.MTU != mtu {
			c.Plugins[i].MTU = mtu
			updated = true
		}

		// 4. 检查并更新 IPAM 配置
		if plugin.IPAM != nil && plugin.IPAM.Subnet != currentPodCIDR {
			c.Plugins[i].IPAM.Subnet = currentPodCIDR
			updated = true
		}
	}

	return updated
}

// IsConfigUpToDate 检查配置是否是最新的
func (c *CNIConfigList) IsUpToDate(currentPodCIDR, serviceCIDR string, mtu int) bool {
	if c == nil || currentPodCIDR == "" {
		return false
	}

	for _, plugin := range c.Plugins {
		if plugin.PodCIDR != currentPodCIDR ||
			plugin.ServiceCIDR != serviceCIDR ||
			plugin.MTU != mtu {
			return false
		}

		if plugin.IPAM != nil && plugin.IPAM.Subnet != currentPodCIDR {
			return false
		}
	}

	return true
}
