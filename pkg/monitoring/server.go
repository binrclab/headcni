package monitoring

import (
	"context"
	"fmt"
	"net/http"

	"github.com/binrclab/headcni/cmd/headcni-daemon/config"
)

// Server 是监控服务器
type Server struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Path    string `yaml:"path"`
	server  *http.Server
}

// NewServer 创建新的监控服务器
func NewServer(monitConfig *config.MonitoringConfig) *Server {

	return &Server{
		Enabled: monitConfig.Enabled,
		Port:    monitConfig.Port,
		Path:    monitConfig.Path,
	}
}

// Start 启动监控服务器
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// 健康检查端点
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 指标端点
	mux.HandleFunc(s.Path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("# HeadCNI metrics\n"))
		w.Write([]byte("headcni_pods_total 0\n"))
		w.Write([]byte("headcni_routes_total 0\n"))
	})

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: mux,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// 记录错误但不退出
		}
	}()

	return nil
}

// Stop 停止监控服务器
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}
