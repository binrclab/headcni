//go:build wireinject
// +build wireinject

package daemon

import (
	"context"

	"github.com/binrclab/headcni/cmd/daemon/config"
	"github.com/google/wire"
)

//go:generate go run github.com/google/wire/cmd/wire

// ProvidePreparer 创建系统准备器
func ProvidePreparer(cfg *config.Config) (*Preparer, func(), error) {
	preparer, err := NewPreparer(cfg)
	if err != nil {
		return nil, nil, err
	}
	return preparer, func() {
		preparer.Shutdown(context.Background())
	}, nil
}

// ProvideDaemonFromPreparer 从 preparer 创建完整的 daemon
func ProvideDaemonFromPreparer(cfg *config.Config, preparer *Preparer) (*Daemon, error) {
	// 创建服务管理器
	serviceManager := NewServiceManager()

	// 注册所有服务
	serviceManager.RegisterService(NewCNIService(preparer))
	serviceManager.RegisterService(NewPodMonitoringService(preparer))
	serviceManager.RegisterService(NewHeadscaleHealthService(preparer))
	serviceManager.RegisterService(NewTailscaleService(preparer))
	serviceManager.RegisterService(NewMonitoringService(preparer))

	// 创建 daemon
	daemon := NewDaemon(cfg, preparer, serviceManager)

	return daemon, nil
}

// ProviderSet is a wire provider set for daemon dependencies
var ProviderSet = wire.NewSet(
	ProvidePreparer,
	ProvideDaemonFromPreparer,
)

// InitDaemon creates a new daemon instance using wire injection
func InitDaemon(cfg *config.Config) (*Daemon, func(), error) {
	panic(wire.Build(ProviderSet))
}
