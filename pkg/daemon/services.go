package daemon

import (
	"context"
	"fmt"
	"sync"

	"github.com/binrclab/headcni/pkg/logging"
)

// Service 定义了可管理服务的接口
type Service interface {
	Name() string
	Start(ctx context.Context) error
	Reload(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool
}

// ServiceManager 管理多个服务
type ServiceManager struct {
	services map[string]Service
	mu       sync.RWMutex
}

// NewServiceManager 创建新的 ServiceManager 实例
func NewServiceManager() *ServiceManager {
	return &ServiceManager{
		services: make(map[string]Service),
	}
}

// RegisterService 注册一个服务
func (sm *ServiceManager) RegisterService(svc Service) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.services[svc.Name()] = svc
}

// StartAll 启动所有注册的服务
func (sm *ServiceManager) StartAll(ctx context.Context) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for name, svc := range sm.services {
		if !svc.IsRunning() {
			logging.Infof("Starting service: %s", name)
			if err := svc.Start(ctx); err != nil {
				return fmt.Errorf("failed to start service %s: %v", name, err)
			}
		}
	}
	return nil
}

// ReloadAll 重载所有注册的服务
func (sm *ServiceManager) ReloadAll(ctx context.Context) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var errs []error
	for name, svc := range sm.services {
		if svc.IsRunning() {
			logging.Infof("Reloading service: %s", name)
			if err := svc.Reload(ctx); err != nil {
				errs = append(errs, fmt.Errorf("failed to reload service %s: %v", name, err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors reloading services: %v", errs)
	}
	return nil
}

// StopAll 停止所有注册的服务
func (sm *ServiceManager) StopAll() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var errs []error
	for name, svc := range sm.services {
		if svc.IsRunning() {
			logging.Infof("Stopping service: %s", name)
			if err := svc.Stop(context.Background()); err != nil {
				errs = append(errs, fmt.Errorf("failed to stop service %s: %v", name, err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors stopping services: %v", errs)
	}
	return nil
}
