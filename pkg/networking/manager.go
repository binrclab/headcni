// pkg/networking/manager.go
package networking

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

type Config struct {
	TailscaleSocket string
	MTU             int
	EnableIPv6      bool
}

type NetworkManager struct {
	config *Config
}

func NewNetworkManager(config *Config) (*NetworkManager, error) {
	if config.MTU == 0 {
		config.MTU = 1420 // 默认 MTU，考虑 Tailscale 开销
	}

	return &NetworkManager{
		config: config,
	}, nil
}

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
