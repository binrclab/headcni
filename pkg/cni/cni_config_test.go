package cni

import (
	"encoding/json"
	"testing"

	"github.com/binrclab/headcni/cmd/daemon/config"
	"github.com/binrclab/headcni/pkg/logging"
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
