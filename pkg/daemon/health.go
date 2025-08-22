package daemon

import (
	"sync"
	"time"
)

// HealthStatus 健康状态
type HealthStatus struct {
	Status    string                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Services  map[string]ServiceInfo `json:"services"`
	Uptime    time.Duration          `json:"uptime"`
}

// ServiceInfo 服务信息
type ServiceInfo struct {
	Name      string    `json:"name"`
	Running   bool      `json:"running"`
	StartTime time.Time `json:"start_time,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// GlobalHealthManager 全局健康管理器
type GlobalHealthManager struct {
	services  map[string]*ServiceInfo
	startTime time.Time
	mu        sync.RWMutex
}

var (
	globalHealth     *GlobalHealthManager
	globalHealthOnce sync.Once
)

// GetGlobalHealthManager 获取全局健康管理器实例
func GetGlobalHealthManager() *GlobalHealthManager {
	globalHealthOnce.Do(func() {
		globalHealth = &GlobalHealthManager{
			services:  make(map[string]*ServiceInfo),
			startTime: time.Now(),
		}
	})
	return globalHealth
}

// RegisterService 注册服务
func (h *GlobalHealthManager) RegisterService(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.services[name] = &ServiceInfo{
		Name:    name,
		Running: false,
	}
}

// UpdateServiceStatus 更新服务状态
func (h *GlobalHealthManager) UpdateServiceStatus(name string, running bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	service, exists := h.services[name]
	if !exists {
		service = &ServiceInfo{Name: name}
		h.services[name] = service
	}

	if running && !service.Running {
		service.StartTime = time.Now()
	}

	service.Running = running
	if err != nil {
		service.Error = err.Error()
	} else {
		service.Error = ""
	}
}

// GetHealthStatus 获取整体健康状态
func (h *GlobalHealthManager) GetHealthStatus() HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := "healthy"
	services := make(map[string]ServiceInfo)

	for name, service := range h.services {
		services[name] = *service
		if !service.Running || service.Error != "" {
			status = "unhealthy"
		}
	}

	return HealthStatus{
		Status:    status,
		Timestamp: time.Now(),
		Services:  services,
		Uptime:    time.Since(h.startTime),
	}
}

// IsHealthy 检查是否健康
func (h *GlobalHealthManager) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, service := range h.services {
		if !service.Running || service.Error != "" {
			return false
		}
	}
	return true
}
