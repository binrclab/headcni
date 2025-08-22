package cni

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/binrclab/headcni/cmd/headcni-daemon/config"
	"github.com/binrclab/headcni/pkg/logging"
)

func TestGenerateConfigList(t *testing.T) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "cni-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建配置管理器
	manager := NewCNIConfigManager(tempDir, "test.conflist", logging.NewSimpleLogger())

	// 创建测试配置
	testConfig := &config.Config{
		Network: config.NetworkConfig{
			MTU:                 1500,
			ServiceCIDR:         "10.96.0.0/12",
			EnableIPv6:          true,
			EnableNetworkPolicy: true,
		},
		IPAM: config.IPAMConfig{
			Type:     "host-local",
			Strategy: "sequential",
		},
		DNS: config.DNSConfig{
			MagicDNS: config.MagicDNSConfig{
				Enabled: true,
				Nameservers: []string{
					"8.8.8.8",
					"8.8.4.4",
				},
				SearchDomains: []string{
					"cluster.local",
					"svc.cluster.local",
				},
			},
		},
	}

	// 生成配置列表
	configList, err := manager.GenerateConfigList("10.244.0.0/16", testConfig)
	if err != nil {
		t.Fatalf("Failed to generate config list: %v", err)
	}

	// 验证基本字段
	if configList.CNIVersion != "1.0.0" {
		t.Errorf("Expected CNIVersion 1.0.0, got %s", configList.CNIVersion)
	}

	if configList.Name != "headcni" {
		t.Errorf("Expected Name headcni, got %s", configList.Name)
	}

	if len(configList.Plugins) != 3 {
		t.Fatalf("Expected 3 plugins, got %d", len(configList.Plugins))
	}

	plugin := configList.Plugins[0]
	if plugin.Type != "headcni" {
		t.Errorf("Expected plugin type headcni, got %s", plugin.Type)
	}

	if plugin.MTU != 1500 {
		t.Errorf("Expected MTU 1500, got %d", plugin.MTU)
	}

	// 验证 IPAM 配置
	if plugin.IPAM == nil {
		t.Fatal("Expected IPAM config, got nil")
	}

	if plugin.IPAM.Type != "host-local" {
		t.Errorf("Expected IPAM type host-local, got %s", plugin.IPAM.Type)
	}

	// 验证 IPAM ranges 配置
	if len(plugin.IPAM.Ranges) != 1 {
		t.Errorf("Expected 1 range, got %d", len(plugin.IPAM.Ranges))
	}
	if len(plugin.IPAM.Ranges[0]) != 1 {
		t.Errorf("Expected 1 subnet in range, got %d", len(plugin.IPAM.Ranges[0]))
	}
	if plugin.IPAM.Ranges[0][0]["subnet"] != "10.244.0.0/16" {
		t.Errorf("Expected subnet 10.244.0.0/16, got %s", plugin.IPAM.Ranges[0][0]["subnet"])
	}

	// 验证其他插件
	portmapPlugin := configList.Plugins[1]
	if portmapPlugin.Type != "portmap" {
		t.Errorf("Expected second plugin type portmap, got %s", portmapPlugin.Type)
	}

	bandwidthPlugin := configList.Plugins[2]
	if bandwidthPlugin.Type != "bandwidth" {
		t.Errorf("Expected third plugin type bandwidth, got %s", bandwidthPlugin.Type)
	}

	// 输出生成的 JSON 用于查看
	jsonData, err := json.MarshalIndent(configList, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	t.Logf("Generated CNI config:\n%s", string(jsonData))
}

func TestGenerateConfigListWithHeadscaleIPAM(t *testing.T) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "cni-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建配置管理器
	manager := NewCNIConfigManager(tempDir, "test-headscale.conflist", logging.NewSimpleLogger())

	// 创建测试配置（使用 headcni-ipam）
	testConfig := &config.Config{
		Network: config.NetworkConfig{
			MTU: 1500,
		},
		IPAM: config.IPAMConfig{
			Type: "headcni-ipam",
		},
	}

	// 生成配置列表
	configList, err := manager.GenerateConfigList("10.244.0.0/16", testConfig)
	if err != nil {
		t.Fatalf("Failed to generate config list: %v", err)
	}

	// 验证 IPAM 类型
	plugin := configList.Plugins[0]
	if plugin.IPAM.Type != "headcni-ipam" {
		t.Errorf("Expected IPAM type headcni-ipam, got %s", plugin.IPAM.Type)
	}

	// 输出生成的 JSON
	jsonData, err := json.MarshalIndent(configList, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	t.Logf("Generated CNI config with headcni-ipam:\n%s", string(jsonData))
}

func TestConfigListValidation(t *testing.T) {
	manager := NewCNIConfigManager("/tmp", "test.conflist", logging.NewSimpleLogger())

	// 测试有效配置
	validConfig := &CNIConfigList{
		CNIVersion: "1.0.0",
		Name:       "test-network",
		Plugins: []CNIPlugin{
			{
				Type: "test-plugin",
			},
		},
	}

	err := manager.ValidateConfigList(validConfig)
	if err != nil {
		t.Errorf("Valid config should pass validation: %v", err)
	}

	// 测试无效配置 - 缺少 CNIVersion
	invalidConfig1 := &CNIConfigList{
		Name: "test-network",
		Plugins: []CNIPlugin{
			{Type: "test-plugin"},
		},
	}

	err = manager.ValidateConfigList(invalidConfig1)
	if err == nil {
		t.Error("Config without CNIVersion should fail validation")
	}

	// 测试无效配置 - 缺少插件
	invalidConfig2 := &CNIConfigList{
		CNIVersion: "1.0.0",
		Name:       "test-network",
		Plugins:    []CNIPlugin{},
	}

	err = manager.ValidateConfigList(invalidConfig2)
	if err == nil {
		t.Error("Config without plugins should fail validation")
	}
}

func TestCNIConfigManagerBackup(t *testing.T) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "cni-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建配置管理器
	manager := NewCNIConfigManager(tempDir, "10-headcni.conflist", nil)

	// 创建测试配置文件
	testFiles := []struct {
		name    string
		content string
	}{
		{
			name: "10-flannel.conflist",
			content: `{
				"cniVersion": "1.0.0",
				"name": "flannel",
				"plugins": [
					{
						"type": "flannel"
					}
				]
			}`,
		},
		{
			name: "20-calico.conf",
			content: `{
				"cniVersion": "1.0.0",
				"name": "calico",
				"type": "calico"
			}`,
		},
		{
			name: "30-weave.json",
			content: `{
				"cniVersion": "1.0.0",
				"name": "weave",
				"type": "weave"
			}`,
		},
		{
			name: "40-canal.yaml",
			content: `{
				"cniVersion": "1.0.0",
				"name": "canal",
				"type": "canal"
			}`,
		},
		{
			name: "other-file.txt",
			content: "This is not a CNI config file",
		},
	}

	// 创建测试文件
	for _, testFile := range testFiles {
		filePath := filepath.Join(tempDir, testFile.name)
		if err := os.WriteFile(filePath, []byte(testFile.content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", testFile.name, err)
		}
	}

	// 测试备份功能
	if err := manager.backupExistingConfigs(); err != nil {
		t.Fatalf("Failed to backup existing configs: %v", err)
	}

	// 检查备份文件是否创建
	backupDir := filepath.Join(tempDir, "backup")
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		t.Fatal("Backup directory was not created")
	}

	// 检查备份文件
	expectedBackups := []string{
		"10-flannel.conflist.headcni_bak",
		"20-calico.conf.headcni_bak",
		"30-weave.json.headcni_bak",
		"40-canal.yaml.headcni_bak",
	}

	for _, expectedBackup := range expectedBackups {
		backupPath := filepath.Join(backupDir, expectedBackup)
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			t.Errorf("Expected backup file not found: %s", expectedBackup)
		}
	}

	// 检查原始文件是否被删除
	for _, testFile := range testFiles {
		if testFile.name == "other-file.txt" {
			// 非 CNI 文件应该保留
			filePath := filepath.Join(tempDir, testFile.name)
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				t.Errorf("Non-CNI file should not be deleted: %s", testFile.name)
			}
		} else {
			// CNI 文件应该被删除
			filePath := filepath.Join(tempDir, testFile.name)
			if _, err := os.Stat(filePath); err == nil {
				t.Errorf("CNI file should be deleted: %s", testFile.name)
			}
		}
	}
}

func TestCNIConfigManagerRestore(t *testing.T) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "cni-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建配置管理器
	manager := NewCNIConfigManager(tempDir, "10-headcni.conflist", nil)

	// 创建备份目录
	backupDir := filepath.Join(tempDir, "backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("Failed to create backup directory: %v", err)
	}

	// 创建测试备份文件
	backupContent := `{
		"cniVersion": "1.0.0",
		"name": "test-network",
		"type": "test-plugin"
	}`

	backupFileName := "test-config.conf.headcni_bak"
	backupPath := filepath.Join(backupDir, backupFileName)
	if err := os.WriteFile(backupPath, []byte(backupContent), 0644); err != nil {
		t.Fatalf("Failed to create backup file: %v", err)
	}

	// 测试恢复功能
	if err := manager.restoreBackup(backupFileName); err != nil {
		t.Fatalf("Failed to restore backup: %v", err)
	}

	// 检查恢复的文件
	restoredPath := filepath.Join(tempDir, "test-config.conf")
	if _, err := os.Stat(restoredPath); os.IsNotExist(err) {
		t.Fatal("Restored file not found")
	}

	// 检查文件内容
	restoredContent, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("Failed to read restored file: %v", err)
	}

	if string(restoredContent) != backupContent {
		t.Errorf("Restored file content does not match backup content")
	}

	// 检查备份文件是否被删除
	if _, err := os.Stat(backupPath); err == nil {
		t.Error("Backup file should be deleted after restore")
	}
}

func TestCNIConfigManagerValidation(t *testing.T) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "cni-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建配置管理器
	manager := NewCNIConfigManager(tempDir, "10-headcni.conflist", nil)

	// 测试有效配置
	validConfig := `{
		"cniVersion": "1.0.0",
		"name": "test-network",
		"plugins": [
			{
				"type": "test-plugin"
			}
		]
	}`

	validPath := filepath.Join(tempDir, "valid.conf")
	if err := os.WriteFile(validPath, []byte(validConfig), 0644); err != nil {
		t.Fatalf("Failed to create valid config file: %v", err)
	}

	if err := manager.validateCNIConfigFile(validPath); err != nil {
		t.Errorf("Valid config should pass validation: %v", err)
	}

	// 测试无效配置
	invalidConfigs := []struct {
		name    string
		content string
	}{
		{
			name:    "missing-cniversion.json",
			content: `{"name": "test", "type": "test"}`,
		},
		{
			name:    "missing-name.json",
			content: `{"cniVersion": "1.0.0", "type": "test"}`,
		},
		{
			name:    "invalid-json.json",
			content: `{"cniVersion": "1.0.0", "name": "test", "type": "test"`,
		},
	}

	for _, invalidConfig := range invalidConfigs {
		invalidPath := filepath.Join(tempDir, invalidConfig.name)
		if err := os.WriteFile(invalidPath, []byte(invalidConfig.content), 0644); err != nil {
			t.Fatalf("Failed to create invalid config file: %v", err)
		}

		if err := manager.validateCNIConfigFile(invalidPath); err == nil {
			t.Errorf("Invalid config should fail validation: %s", invalidConfig.name)
		}
	}
}
