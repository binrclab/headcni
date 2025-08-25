package daemon

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"net/netip"

	"github.com/binrclab/headcni/pkg/logging"
	"github.com/tailscale/netlink"
)

func TestInterfaceNameGeneration(t *testing.T) {
	tests := []struct {
		nodeName    string
		expected    string
		description string
	}{
		{
			nodeName:    "cn-guizhou-worker-001d",
			expected:    "headcni-cn-guizhou",
			description: "Normal node name with dots",
		},
		{
			nodeName:    "worker-001",
			expected:    "headcni-worker-001",
			description: "Node name without dots",
		},
		{
			nodeName:    "very-long-node-name-that-exceeds-fifteen-characters",
			expected:    "headcni-very-long",
			description: "Node name exceeding 15 characters",
		},
		{
			nodeName:    "node.with.many.dots",
			expected:    "headcni-node-with",
			description: "Node name with many dots",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			// 模拟接口名称生成逻辑
			interfaceName := fmt.Sprintf("headcni-%s", strings.ReplaceAll(tt.nodeName, ".", "-"))

			// 确保接口名称符合 Linux 接口命名规范（最多 15 个字符）
			if len(interfaceName) > 15 {
				interfaceName = interfaceName[:15]
			}

			if interfaceName != tt.expected {
				t.Errorf("Expected interface name %s, got %s", tt.expected, interfaceName)
			}

			// 验证长度不超过 15 个字符
			if len(interfaceName) > 15 {
				t.Errorf("Interface name length %d exceeds 15 characters: %s", len(interfaceName), interfaceName)
			}

			// 验证不包含点号
			if strings.Contains(interfaceName, ".") {
				t.Errorf("Interface name contains dots: %s", interfaceName)
			}
		})
	}
}

func TestInterfaceNameValidation(t *testing.T) {
	// 测试有效的接口名称
	validNames := []string{
		"headcni-node1",
		"headcni-worker",
		"headcni-master",
		"eth0",
		"tailscale0",
	}

	for _, name := range validNames {
		if len(name) > 15 {
			t.Errorf("Interface name %s exceeds 15 characters", name)
		}
		if strings.Contains(name, ".") {
			t.Errorf("Interface name %s contains dots", name)
		}
	}

	// 测试无效的接口名称
	invalidNames := []string{
		"headcni-node.with.dots",
		"very-long-interface-name-that-exceeds-fifteen-characters",
	}

	for _, name := range invalidNames {
		if len(name) > 15 || strings.Contains(name, ".") {
			t.Logf("Invalid interface name (as expected): %s", name)
		}
	}
}

// 测试函数，用于调试 IP Rule 添加问题
// 测试函数，用于调试 IP Rule 添加问题
func TestIPRule(t *testing.T) {
	fmt.Println("=== IP Rule 测试开始 ===")

	// 1. 测试 IP 地址解析
	testIP := "10.2.43.187" // 替换为你的实际 Tailscale IP
	tailscaleIP, err := netip.ParseAddr(testIP)
	if err != nil {
		fmt.Printf("❌ IP 解析失败: %v\n", err)
		return
	}
	fmt.Printf("✅ IP 解析成功: %s\n", tailscaleIP.String())

	// 检查当前规则列表
	rules, err := netlink.RuleList(netlink.FAMILY_V4)
	if err != nil {
		logging.Warnf("Failed to get rules: %v", err)
		return
	}

	// 检查是否已存在完全匹配的规则
	for _, rule := range rules {
		if rule.Priority == 153 && rule.Table == 53 && rule.Src != nil &&
			rule.Src.IP.Equal(tailscaleIP.AsSlice()) &&
			rule.Src.Mask.String() == net.CIDRMask(32, 32).String() {
			logging.Infof("Rule already exists: from %s lookup 53 priority 153", tailscaleIP)
			return
		}
	}

	// 删除同网段的旧规则
	var rulesToDelete []*netlink.Rule
	for _, rule := range rules {
		if rule.Priority == 153 && rule.Table == 53 && rule.Src != nil {
			if isSameNetwork(rule.Src.IP, tailscaleIP.AsSlice()) && !rule.Src.IP.Equal(tailscaleIP.AsSlice()) {
				rulesToDelete = append(rulesToDelete, &rule)
				logging.Infof("Found old rule in same network to delete: from %s lookup 53 priority 153",
					rule.Src.IP.String())
			}
		}
	}

	for _, rule := range rulesToDelete {
		if err := netlink.RuleDel(rule); err != nil {
			logging.Warnf("Failed to delete old rule %v: %v", rule, err)
		} else {
			logging.Infof("Deleted old rule: from %s lookup 53 priority 153",
				rule.Src.IP.String())
		}
	}

	// 只保留一种添加规则方式（CIDRMask + 掩码对齐）
	mask := net.CIDRMask(32, 32)

	// 把 netip.Addr 转换为 net.IP
	ip := net.IP(tailscaleIP.AsSlice()).To4()
	if ip == nil {
		logging.Warnf("invalid IPv4 address: %s", tailscaleIP)
		return
	}

	rule := netlink.NewRule()
	rule.Src = &net.IPNet{
		IP:   ip,
		Mask: mask,
	}
	rule.Table = 53
	rule.Priority = 153
	logging.Infof("Attempting to add rule: from %s lookup 53 priority 153", tailscaleIP)

	if err := netlink.RuleAdd(rule); err != nil {
		logging.Warnf("Failed to add rule: %v", err)
	} else {
		logging.Infof("Successfully added rule: from %s lookup 53 priority 153", tailscaleIP)
	}
	return
}
