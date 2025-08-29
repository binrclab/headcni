package cni

import (
	"encoding/json"
	"strings"
	"testing"

	"os"
	"path/filepath"

	"github.com/binrclab/headcni/cmd/daemon/config"
	"github.com/binrclab/headcni/pkg/logging"
	"github.com/binrclab/yamlc"
)

func TestGenerateConfigList(t *testing.T) {

	// 创建配置管理器
	manager := NewCNIConfigManager("/tmp/cni-test189451373", "test.conflist", "/tmp/env.yaml", logging.NewSimpleLogger())

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
		CNIPlugins: []config.CNIPluginsConfig{
			{
				Name:     "bandwidth",
				Priority: 1,
				Enabled:  true,
				Config:   "{\"bandwidth\": 1000000000, \"burst\": 1000000000}",
			},
			{
				Name:     "portmap",
				Priority: 2,
				Enabled:  true,
				Config:   "{\"portMappings\": [{\"hostPort\": 8080, \"containerPort\": 8080, \"protocol\": \"tcp\"}]}",
			},
		},
	}

	// 生成配置列表
	configList, cniEnv, err := manager.GenerateConfigList("10.244.0.0/16", testConfig, "10.96.0.10", "cluster.local")
	if err != nil {
		t.Fatalf("Failed to generate config list: %v", err)
	}

	// 验证基本字段
	if configList.CNIVersion != "1.0.0" {
		t.Errorf("Expected CNIVersion 1.0.0, got %s", configList.CNIVersion)
	}

	if configList.Name != "cbr0" {
		t.Errorf("Expected Name cbr0, got %s", configList.Name)
	}

	if len(configList.Plugins) < 1 {
		t.Fatalf("Expected at least 1 plugin, got %d", len(configList.Plugins))
	}

	// 验证 headcni 插件
	headcniPlugin := configList.Plugins[0]
	if headcniPlugin["type"] != "headcni" {
		t.Errorf("Expected first plugin type headcni, got %s", headcniPlugin["type"])
	}

	// 验证插件数量
	expectedPluginCount := 1 + len(testConfig.CNIPlugins) // headcni + 其他插件
	if len(configList.Plugins) != expectedPluginCount {
		t.Errorf("Expected %d plugins, got %d", expectedPluginCount, len(configList.Plugins))
		t.Logf("Plugins found:")
		for i, plugin := range configList.Plugins {
			t.Logf("  Plugin %d: %v", i, plugin)
		}
	}

	// 验证其他插件
	if len(configList.Plugins) > 1 {
		bandwidthPlugin := configList.Plugins[1]
		if bandwidthPlugin["type"] != "bandwidth" {
			t.Errorf("Expected second plugin type bandwidth, got %s", bandwidthPlugin["type"])
		}
	}

	if len(configList.Plugins) > 2 {
		portmapPlugin := configList.Plugins[2]
		if portmapPlugin["type"] != "portmap" {
			t.Errorf("Expected third plugin type portmap, got %s", portmapPlugin["type"])
		}
	}

	// 验证 cniEnv
	if cniEnv == nil {
		t.Fatal("Expected cniEnv, got nil")
	}

	if cniEnv.NetWork != "10.96.0.0/12" {
		t.Errorf("Expected Network 10.96.0.0/12, got %s", cniEnv.NetWork)
	}

	if cniEnv.Subnet != "10.244.0.0/16" {
		t.Errorf("Expected Subnet 10.244.0.0/16, got %s", cniEnv.Subnet)
	}

	if cniEnv.MTU != 1500 {
		t.Errorf("Expected MTU 1500, got %d", cniEnv.MTU)
	}

	// 输出生成的配置用于查看
	jsonData, err := json.MarshalIndent(configList, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	t.Logf("Generated CNI config:\n%s", string(jsonData))

	// 输出生成的 cniEnv 用于查看
	envData, err := json.MarshalIndent(cniEnv, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal cniEnv: %v", err)
	}

	t.Logf("Generated CNI env:\n%s", string(envData))

	// 写入配置
	if err := manager.WriteConfigListAndEnv(configList, cniEnv); err != nil {
		t.Fatalf("Failed to write config and env: %v", err)
	}

	// 读取配置
	readConfig, err := manager.ReadConfigList()
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	jsonData, err = json.MarshalIndent(readConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	t.Logf("Read CNI config:\n%s", string(jsonData))

	// 读取 cniEnv
	readEnv, err := manager.ReadCniEnv()
	if err != nil {
		t.Fatalf("Failed to read cniEnv: %v", err)
	}

	t.Logf("Read CNI env:\n%vCA", readEnv)
}

// TestYamlcGeneration 测试 yamlc 库生成的 YAML 是否正确
func TestYamlcGeneration(t *testing.T) {
	// 创建测试用的 CniEnv 结构体
	testCniEnv := &CniEnv{
		NetWork: "10.96.0.0/12",
		Subnet:  "10.244.0.0/16",
		IPv6Net: "fd00::/108",
		IPv6Sub: "fd00:244::/64",
		MTU:     1500,
		IPMasq:  true,
		Metadata: &Metadata{
			GeneratedAt: "2025-01-28T10:00:00Z",
			NodeName:    "test-node",
			ClusterCIDR: "10.244.0.0/16",
			ServiceCIDR: "10.96.0.0/12",
		},
		Routes: []Route{
			{
				Dst: "10.96.0.0/12",
				GW:  "10.244.0.1",
			},
			{
				Dst: "fd00::/108",
				GW:  "fd00:244::1",
			},
		},
		DNS: &DNS{
			Nameservers: []string{"10.96.0.10", "8.8.8.8"},
			Search:      []string{"cluster.local", "svc.cluster.local"},
			Options:     []string{"ndots:5", "timeout:2"},
		},
		Policies: &Policies{
			AllowHostAccess:     true,
			AllowServiceAccess:  true,
			AllowExternalAccess: false,
		},
	}

	// 使用 yamlc 生成 YAML
	yamlData, err := yamlc.Gen(testCniEnv, yamlc.WithStyle(yamlc.StyleInline))
	if err != nil {
		t.Fatalf("Failed to generate YAML with yamlc: %v", err)
	}

	// 输出生成的 YAML 用于查看
	t.Logf("Generated YAML with yamlc:\n%s", string(yamlData))

	// 验证生成的 YAML 包含必要的字段
	yamlStr := string(yamlData)

	// 检查基本字段
	expectedFields := []string{
		"network: 10.96.0.0/12",
		"subnet: 10.244.0.0/16",
		"ipv6_network: fd00::/108",
		"ipv6_subnet: fd00:244::/64",
		"mtu: 1500",
		"ipmasq: true",
	}

	for _, field := range expectedFields {
		if !strings.Contains(yamlStr, field) {
			t.Errorf("Expected YAML to contain: %s", field)
		}
	}

	// 检查注释是否正确生成
	expectedComments := []string{
		"# IPv4 network configuration (Service CIDR)",
		"# IPv4 subnet configuration",
		"# IPv6 network configuration (Service CIDR)",
		"# IPv6 subnet configuration",
		"# MTU configuration",
		"# IP masquerade configuration",
	}

	for _, comment := range expectedComments {
		if !strings.Contains(yamlStr, comment) {
			t.Errorf("Expected YAML to contain comment: %s", comment)
		}
	}

	// 检查 metadata 部分
	if !strings.Contains(yamlStr, "metadata:") {
		t.Error("Expected YAML to contain metadata section")
	}

	if !strings.Contains(yamlStr, "generated_at: 2025-01-28T10:00:00Z") {
		t.Error("Expected YAML to contain generated_at field")
	}

	// 检查 routes 部分
	if !strings.Contains(yamlStr, "routes:") {
		t.Error("Expected YAML to contain routes section")
	}

	if !strings.Contains(yamlStr, "dst: 10.96.0.0/12") {
		t.Error("Expected YAML to contain route destination")
	}

	// 检查 DNS 部分
	if !strings.Contains(yamlStr, "dns:") {
		t.Error("Expected YAML to contain DNS section")
	}

	if !strings.Contains(yamlStr, "nameservers:") {
		t.Error("Expected YAML to contain DNS nameservers")
	}

	// 测试不同的注释样式
	t.Run("StyleInline", func(t *testing.T) {
		yamlData, err := yamlc.Gen(testCniEnv, yamlc.WithStyle(yamlc.StyleInline))
		if err != nil {
			t.Fatalf("Failed to generate YAML with StyleInline: %v", err)
		}

		yamlStr := string(yamlData)
		t.Logf("StyleInline YAML:\n%s", yamlStr)

		// 检查内联注释格式
		if !strings.Contains(yamlStr, "network: 10.96.0.0/12") {
			t.Error("Expected inline style to contain network field")
		}
	})

	t.Run("StyleSmart", func(t *testing.T) {
		yamlData, err := yamlc.Gen(testCniEnv, yamlc.WithStyle(yamlc.StyleSmart))
		if err != nil {
			t.Fatalf("Failed to generate YAML with StyleSmart: %v", err)
		}

		yamlStr := string(yamlData)
		t.Logf("StyleSmart YAML:\n%s", yamlStr)

		// 检查智能样式格式
		if !strings.Contains(yamlStr, "network: 10.96.0.0/12") {
			t.Error("Expected smart style to contain network field")
		}
	})

	t.Run("StyleCompact", func(t *testing.T) {
		yamlData, err := yamlc.Gen(testCniEnv, yamlc.WithStyle(yamlc.StyleCompact))
		if err != nil {
			t.Fatalf("Failed to generate YAML with StyleCompact: %v", err)
		}

		yamlStr := string(yamlData)
		t.Logf("StyleCompact YAML:\n%s", yamlStr)

		// 检查紧凑样式格式
		if !strings.Contains(yamlStr, "network: 10.96.0.0/12") {
			t.Error("Expected compact style to contain network field")
		}
	})

	// 测试写入文件功能
	t.Run("WriteToFile", func(t *testing.T) {
		// 创建临时目录
		tempDir := t.TempDir()
		tempFile := filepath.Join(tempDir, "test_env.yaml")

		// 创建配置管理器
		manager := NewCNIConfigManager(tempDir, "test.conflist", tempFile, logging.NewSimpleLogger())

		// 写入文件
		if err := manager.WriteCniEnv(testCniEnv); err != nil {
			t.Fatalf("Failed to write CniEnv to file: %v", err)
		}

		// 读取文件验证内容
		readEnv, err := manager.ReadCniEnv()
		if err != nil {
			t.Fatalf("Failed to read CniEnv from file: %v", err)
		}

		// 验证读取的数据是否正确
		if readEnv.NetWork != testCniEnv.NetWork {
			t.Errorf("Expected Network %s, got %s", testCniEnv.NetWork, readEnv.NetWork)
		}

		if readEnv.Subnet != testCniEnv.Subnet {
			t.Errorf("Expected Subnet %s, got %s", testCniEnv.Subnet, readEnv.Subnet)
		}

		if readEnv.MTU != testCniEnv.MTU {
			t.Errorf("Expected MTU %d, got %d", testCniEnv.MTU, readEnv.MTU)
		}

		// 输出文件内容
		fileContent, err := os.ReadFile(tempFile)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}

		t.Logf("File content:\n%s", string(fileContent))
	})
}
