package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/binrclab/headcni/cmd/daemon/config"
	"github.com/binrclab/headcni/pkg/logging"
)

// Daemon 是 HeadCNI 守护进程
type Daemon struct {
	// 配置和控制
	config   *config.Config
	preparer *Preparer
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// 服务管理器
	serviceManager *ServiceManager
}

func NewDaemon(
	cfg *config.Config,
	preparer *Preparer,
	serviceManager *ServiceManager,
) *Daemon {
	daemon := &Daemon{
		config:         cfg,
		preparer:       preparer,
		serviceManager: serviceManager,
	}

	return daemon
}

// Start 启动守护进程
func (d *Daemon) Start() error {
	return d.StartWithContext(context.Background())
}

// StartWithContext 使用指定上下文启动守护进程
func (d *Daemon) StartWithContext(ctx context.Context) error {
	d.ctx, d.cancel = context.WithCancel(ctx)

	logging.Infof("Starting HeadCNI daemon")

	// 启动所有注册的服务
	if err := d.serviceManager.StartAll(d.ctx); err != nil {
		return fmt.Errorf("failed to start services: %v", err)
	}

	// 等待退出信号
	qc := make(chan os.Signal, 1)
	var sig os.Signal
	signal.Notify(qc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// 启动上下文监听
	go func() { <-d.ctx.Done(); qc <- syscall.SIGTERM }()

	for sig = <-qc; sig == syscall.SIGHUP; sig = <-qc {
		d.reloadConfigAndServices()
	}

	// 优雅关闭服务
	logging.Infof("Received signal: %s, starting graceful shutdown...", sig.String())
	d.serviceManager.StopAll()
	logging.Infof("HeadCNI daemon stopped")

	return nil
}

// Stop 停止守护进程
func (d *Daemon) Stop() error {
	logging.Infof("Stopping HeadCNI daemon")

	// 停止所有服务
	if d.serviceManager != nil {
		if err := d.serviceManager.StopAll(); err != nil {
			logging.Errorf("Failed to stop services: %v", err)
		}
	}

	// 停止所有协程
	if d.cancel != nil {
		d.cancel()
	}

	// 等待所有协程结束
	d.wg.Wait()

	logging.Infof("HeadCNI daemon stopped")
	return nil
}

// GetServiceManager 获取服务管理器（供外部查询使用）
func (d *Daemon) GetServiceManager() *ServiceManager {
	return d.serviceManager
}

// reloadConfigAndServices 重新加载配置和服务
func (d *Daemon) reloadConfigAndServices() error {
	// 1. 重新加载配置
	configChanged, err := d.preparer.ReloadConfig()
	if err != nil {
		return fmt.Errorf("failed to reload config: %v", err)
	}

	if !configChanged {
		logging.Infof("配置未发生变化，无需重载服务")
		return nil
	}

	// 3. 优雅重启服务
	logging.Infof("配置已变更，开始重载服务...")
	if err := d.reloadServices(); err != nil {
		return fmt.Errorf("failed to reload services: %v", err)
	}

	return nil
}

// reloadServices 重载服务
func (d *Daemon) reloadServices() error {
	logging.Infof("开始重载服务...")

	// 使用服务的 Reload 方法进行热重载
	if err := d.serviceManager.ReloadAll(d.ctx); err != nil {
		logging.Errorf("热重载失败，尝试重启服务: %v", err)

		// 热重载失败时，回退到停止-启动方式
		logging.Infof("停止现有服务...")
		d.serviceManager.StopAll()

		// 等待服务完全停止
		time.Sleep(2 * time.Second)

		// 使用新配置重新启动服务
		logging.Infof("使用新配置启动服务...")
		if err := d.serviceManager.StartAll(d.ctx); err != nil {
			return fmt.Errorf("服务重启失败: %v", err)
		}
	}

	logging.Infof("服务重载完成")
	return nil
}
