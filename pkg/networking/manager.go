// pkg/networking/manager.go
package networking

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

// Config 定义网络管理器配置
type Config struct {
	TailscaleSocket string
	MTU             int
	EnableIPv6      bool
}

// NetworkManager 是网络管理器
type NetworkManager struct {
	config *Config
}

// NewNetworkManager 创建新的网络管理器
func NewNetworkManager(config *Config) (*NetworkManager, error) {
	if config.MTU == 0 {
		config.MTU = 1420 // 默认 MTU，考虑 Tailscale 封装开销
	}

	return &NetworkManager{
		config: config,
	}, nil
}

// CreateVethPair 创建 veth pair
func (nm *NetworkManager) CreateVethPair(netnsPath, containerIfName, hostIfName string) error {
	// 创建 veth pair
	hostVeth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: hostIfName,
			MTU:  nm.config.MTU,
		},
		PeerName: containerIfName,
	}

	if err := netlink.LinkAdd(hostVeth); err != nil {
		return fmt.Errorf("failed to create veth pair: %v", err)
	}

	// 获取 peer 接口（容器端）
	containerVeth, err := netlink.LinkByName(containerIfName)
	if err != nil {
		// 清理已创建的 host veth
		netlink.LinkDel(hostVeth)
		return fmt.Errorf("failed to find container veth: %v", err)
	}

	// 将容器端接口移动到目标网络命名空间
	containerNS, err := ns.GetNS(netnsPath)
	if err != nil {
		netlink.LinkDel(hostVeth)
		return fmt.Errorf("failed to get container netns: %v", err)
	}
	defer containerNS.Close()

	if err := netlink.LinkSetNsFd(containerVeth, int(containerNS.Fd())); err != nil {
		netlink.LinkDel(hostVeth)
		return fmt.Errorf("failed to move veth to container netns: %v", err)
	}

	klog.V(4).Infof("Created veth pair: %s (host) <-> %s (container)",
		hostIfName, containerIfName)

	return nil
}

// GetTailscaleIP 获取 Tailscale IP
func (nm *NetworkManager) GetTailscaleIP() (net.IP, error) {
	// 从 tailscale status 获取本机 IP
	cmd := exec.Command("tailscale", "ip", "-4")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get tailscale IP: %v", err)
	}

	ipStr := strings.TrimSpace(string(output))
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ipStr)
	}

	return ip, nil
}

// GetTailscaleStatus 获取 Tailscale 状态
func (nm *NetworkManager) GetTailscaleStatus() (*TailscaleStatus, error) {
	cmd := exec.Command("tailscale", "status", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get tailscale status: %v", err)
	}

	var status TailscaleStatus
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, fmt.Errorf("failed to parse tailscale status: %v", err)
	}

	return &status, nil
}

// CheckTailscaleConnectivity 检查 Tailscale 连接性
func (nm *NetworkManager) CheckTailscaleConnectivity() error {
	status, err := nm.GetTailscaleStatus()
	if err != nil {
		return err
	}

	if status.BackendState != "Running" {
		return fmt.Errorf("tailscale not running, state: %s", status.BackendState)
	}

	if !status.Self.Online {
		return fmt.Errorf("tailscale offline")
	}

	if len(status.Self.TailscaleIPs) == 0 {
		return fmt.Errorf("no tailscale IP assigned")
	}

	klog.V(4).Infof("Tailscale connectivity OK, IP: %s",
		strings.Join(status.Self.TailscaleIPs, ","))

	return nil
}

// InterfaceExists 检查接口是否存在
func (nm *NetworkManager) InterfaceExists(interfaceName string) bool {
	_, err := netlink.LinkByName(interfaceName)
	return err == nil
}

// CreateInterface 创建网络接口
func (nm *NetworkManager) CreateInterface(interfaceName string, interfaceType string) error {
	var link netlink.Link

	switch interfaceType {
	case "dummy":
		link = &netlink.Dummy{
			LinkAttrs: netlink.LinkAttrs{
				Name: interfaceName,
			},
		}
	case "bridge":
		link = &netlink.Bridge{
			LinkAttrs: netlink.LinkAttrs{
				Name: interfaceName,
			},
		}
	default:
		return fmt.Errorf("unsupported interface type: %s", interfaceType)
	}

	if err := netlink.LinkAdd(link); err != nil {
		return fmt.Errorf("failed to create interface %s: %v", interfaceName, err)
	}

	klog.V(4).Infof("Created interface: %s (type: %s)", interfaceName, interfaceType)
	return nil
}

// ConfigureInterface 配置网络接口
func (nm *NetworkManager) ConfigureInterface(interfaceName string, ip net.IP, mask net.IPMask) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return fmt.Errorf("failed to find interface %s: %v", interfaceName, err)
	}

	// 配置 IP 地址
	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: mask,
		},
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("failed to add IP to interface %s: %v", interfaceName, err)
	}

	// 启用接口
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to enable interface %s: %v", interfaceName, err)
	}

	klog.V(4).Infof("Configured interface: %s with IP %s", interfaceName, ip.String())
	return nil
}

// DeleteInterface 删除网络接口
func (nm *NetworkManager) DeleteInterface(interfaceName string) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		// 接口不存在，认为删除成功
		return nil
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete interface %s: %v", interfaceName, err)
	}

	klog.V(4).Infof("Deleted interface: %s", interfaceName)
	return nil
}

// StartTailscaleService 启动 Tailscale 服务
func (nm *NetworkManager) StartTailscaleService(interfaceName, nodeKey, headscaleURL string) error {
	// 检查接口是否已存在
	if nm.InterfaceExists(interfaceName) {
		klog.V(4).Infof("Interface %s already exists", interfaceName)
		return nil
	}

	// 创建专用的 tailscaled 配置目录
	configDir := fmt.Sprintf("/var/lib/headcni/tailscale/%s", interfaceName)
	if err := exec.Command("mkdir", "-p", configDir).Run(); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	// 创建 socket 目录
	socketDir := fmt.Sprintf("/var/run/headcni/tailscale")
	if err := exec.Command("mkdir", "-p", socketDir).Run(); err != nil {
		return fmt.Errorf("failed to create socket directory: %v", err)
	}

	// 启动专用的 tailscaled 进程
	cmd := exec.Command("tailscaled",
		"--state", fmt.Sprintf("%s/tailscaled.state", configDir),
		"--socket", fmt.Sprintf("%s/%s.sock", socketDir, interfaceName),
		"--tun", interfaceName,
		"--port", "0", // 随机端口
		"--verbose", "1",
	)

	// 设置环境变量避免与系统 Tailscale 冲突
	cmd.Env = append(os.Environ(),
		"TAILSCALE_USE_WIPEOUT=true",
		"TAILSCALE_DEBUG=1",
	)

	// 启动进程
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tailscaled: %v", err)
	}

	// 等待服务启动
	time.Sleep(3 * time.Second)

	// 检查进程是否还在运行
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return fmt.Errorf("tailscaled process exited unexpectedly")
	}

	// 使用 nodekey 连接到 Headscale
	upCmd := exec.Command("tailscale", "up",
		"--authkey", nodeKey,
		"--hostname", fmt.Sprintf("headcni-%s", interfaceName),
		"--socket", fmt.Sprintf("%s/%s.sock", socketDir, interfaceName),
		"--accept-dns=false",
		"--accept-routes=true",
		"--advertise-routes", "10.244.0.0/16", // 根据实际 Pod CIDR 调整
	)

	if err := upCmd.Run(); err != nil {
		// 清理进程
		cmd.Process.Kill()
		return fmt.Errorf("failed to connect to headscale: %v", err)
	}

	klog.V(4).Infof("Started tailscale service for interface: %s (PID: %d)", interfaceName, cmd.Process.Pid)
	return nil
}

// StopTailscaleService 停止 Tailscale 服务
func (nm *NetworkManager) StopTailscaleService(interfaceName string) error {
	socketPath := fmt.Sprintf("/var/run/headcni/tailscale/%s.sock", interfaceName)

	// 停止 tailscale 连接
	downCmd := exec.Command("tailscale", "down",
		"--socket", socketPath,
	)
	downCmd.Run() // 忽略错误

	// 查找并停止相关的 tailscaled 进程
	// 使用 ps 查找包含特定 socket 的 tailscaled 进程
	psCmd := exec.Command("ps", "aux")
	output, err := psCmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "tailscaled") && strings.Contains(line, interfaceName) {
				fields := strings.Fields(line)
				if len(fields) > 1 {
					pid := fields[1]
					killCmd := exec.Command("kill", "-TERM", pid)
					killCmd.Run()
					klog.V(4).Infof("Sent TERM signal to tailscaled process: %s", pid)
				}
			}
		}
	}

	// 等待进程退出
	time.Sleep(2 * time.Second)

	// 强制杀死残留进程
	exec.Command("pkill", "-f", fmt.Sprintf("tailscaled.*%s", interfaceName)).Run()

	// 清理 socket 文件
	exec.Command("rm", "-f", socketPath).Run()

	klog.V(4).Infof("Stopped tailscale service for interface: %s", interfaceName)
	return nil
}

// TailscaleStatus 表示 Tailscale 状态
type TailscaleStatus struct {
	BackendState string `json:"BackendState"`
	Self         struct {
		ID           string   `json:"ID"`
		HostName     string   `json:"HostName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
		Online       bool     `json:"Online"`
	} `json:"Self"`
	Peers map[string]struct {
		ID            string   `json:"ID"`
		HostName      string   `json:"HostName"`
		TailscaleIPs  []string `json:"TailscaleIPs"`
		Online        bool     `json:"Online"`
		LastSeen      string   `json:"LastSeen"`
		PrimaryRoutes struct {
			Advertised []string `json:"advertised"`
			Enabled    []string `json:"enabled"`
		} `json:"PrimaryRoutes"`
	} `json:"Peers"`
}
