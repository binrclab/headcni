package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/binrclab/headcni/pkg/daemon"
	"github.com/binrclab/headcni/pkg/logging"
)

var (
	headscaleURL       = flag.String("headscale-url", "", "Headscale server URL")
	headscaleAuthKey   = flag.String("headscale-auth-key", "", "Headscale API key")
	podCIDR            = flag.String("pod-cidr", "10.244.0.0/16", "Pod CIDR")
	serviceCIDR        = flag.String("service-cidr", "10.96.0.0/16", "Service CIDR")
	mtu                = flag.Int("mtu", 1280, "MTU size")
	ipamType           = flag.String("ipam-type", "host-local", "IPAM type")
	allocationStrategy = flag.String("allocation-strategy", "sequential", "IP allocation strategy")
	metricsPort        = flag.Int("metrics-port", 8080, "Metrics server port")
	metricsPath        = flag.String("metrics-path", "/metrics", "Metrics server path")
	mode               = flag.String("mode", "host", "HeadCNI mode: host or daemon")
	interfaceName      = flag.String("interface-name", "headcni01", "Tailscale interface name (daemon mode)")
)

func main() {
	flag.Parse()

	// 设置日志
	logger := logging.NewLogger()
	logger.Info("Starting HeadCNI Daemon",
		"headscale_url", *headscaleURL,
		"pod_cidr", *podCIDR,
		"mode", *mode,
		"interface_name", *interfaceName,
	)

	// 创建 Kubernetes 客户端
	k8sClient, err := createK8sClient()
	if err != nil {
		logger.Error("Failed to create Kubernetes client", "error", err)
		os.Exit(1)
	}

	// 创建 Daemon 配置
	config := &daemon.Config{
		HeadscaleURL:       *headscaleURL,
		HeadscaleAuthKey:   *headscaleAuthKey,
		PodCIDR:            *podCIDR,
		ServiceCIDR:        *serviceCIDR,
		MTU:                *mtu,
		IPAMType:           *ipamType,
		AllocationStrategy: *allocationStrategy,
		MetricsPort:        *metricsPort,
		MetricsPath:        *metricsPath,
		Mode:               *mode,
		InterfaceName:      *interfaceName,
		K8sClient:          k8sClient,
		Logger:             logger,
	}

	// 创建并启动 Daemon
	d, err := daemon.New(config)
	if err != nil {
		logger.Error("Failed to create daemon", "error", err)
		os.Exit(1)
	}

	// 启动 Daemon
	go func() {
		if err := d.Start(); err != nil {
			logger.Error("Daemon failed", "error", err)
			os.Exit(1)
		}
	}()

	// 等待信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down HeadCNI Daemon")

	// 优雅关闭
	if err := d.Stop(); err != nil {
		logger.Error("Error during shutdown", "error", err)
	}
}

func createK8sClient() (*kubernetes.Clientset, error) {
	// 尝试集群内配置
	config, err := rest.InClusterConfig()
	if err != nil {
		// 尝试 kubeconfig 文件
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = os.Getenv("HOME") + "/.kube/config"
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create k8s config: %v", err)
		}
	}

	return kubernetes.NewForConfig(config)
}
