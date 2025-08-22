package cni

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/binrclab/headcni/pkg/logging"
)

// Server 是 CNI 服务器（使用函数回调，去掉 Handler 接口以简化结构）
type Server struct {
	socketPath string
	server     *http.Server
	onAllocate func(*CNIRequest) *CNIResponse
	onRelease  func(*CNIRequest) *CNIResponse
	onStatus   func(*CNIRequest) *CNIResponse
	onPodReady func(*CNIRequest) *CNIResponse
}

// NewServer 创建新的 CNI 服务器（使用默认回调）
func NewServer(socketPath string) *Server {
	s := &Server{socketPath: socketPath}
	s.setDefaultCallbacks()
	return s
}

// NewServerWithCallbacks 创建带回调的 CNI 服务器
func NewServerWithCallbacks(socketPath string, onAllocate, onRelease, onStatus, onPodReady func(*CNIRequest) *CNIResponse) *Server {
	s := &Server{
		socketPath: socketPath,
		onAllocate: onAllocate,
		onRelease:  onRelease,
		onStatus:   onStatus,
		onPodReady: onPodReady,
	}
	s.setDefaultCallbacks()
	return s
}

func (s *Server) setDefaultCallbacks() {
	if s.onAllocate == nil {
		s.onAllocate = func(req *CNIRequest) *CNIResponse {
			return &CNIResponse{Success: true}
		}
	}
	if s.onRelease == nil {
		s.onRelease = func(req *CNIRequest) *CNIResponse { return &CNIResponse{Success: true} }
	}
	if s.onStatus == nil {
		s.onStatus = func(req *CNIRequest) *CNIResponse {
			return &CNIResponse{Success: true, Data: map[string]interface{}{"status": "ready"}}
		}
	}
	if s.onPodReady == nil {
		s.onPodReady = func(req *CNIRequest) *CNIResponse {
			return &CNIResponse{Success: true, Data: map[string]interface{}{"ready": true}}
		}
	}
}

// Start 启动 CNI 服务器
func (s *Server) Start() error {
	// 准备 socket 目录
	if err := s.prepareSocketDirectory(); err != nil {
		return err
	}

	// 创建并启动 HTTP 服务器
	return s.createAndStartHTTPServer()
}

// Stop 停止 CNI 服务器
func (s *Server) Stop() error {
	// 删除 socket 文件
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove socket file: %v", err)
	}
	return nil
}

// prepareSocketDirectory 准备 socket 目录
func (s *Server) prepareSocketDirectory() error {
	// 创建 socket 目录
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %v", err)
	}

	// 删除已存在的 socket 文件
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %v", err)
	}

	return nil
}

// createAndStartHTTPServer 创建并启动 HTTP 服务器
func (s *Server) createAndStartHTTPServer() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/cni", s.handleCNIRequest)

	s.server = &http.Server{
		Addr:    "unix://" + s.socketPath,
		Handler: mux,
	}

	// 启动服务器
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Errorf("CNI server error: %v", err)
		}
	}()

	logging.Infof("CNI server started: %s", s.socketPath)
	return nil
}

// handleCNIRequest 处理 CNI 请求
func (s *Server) handleCNIRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CNIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	resp := s.processCNIRequest(&req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// processCNIRequest 处理 CNI 请求
func (s *Server) processCNIRequest(req *CNIRequest) *CNIResponse {
	switch req.Type {
	case "allocate":
		return s.onAllocate(req)
	case "release":
		return s.onRelease(req)
	case "status":
		return s.onStatus(req)
	case "pod_ready":
		return s.onPodReady(req)
	default:
		return &CNIResponse{
			Success: false,
			Error:   fmt.Sprintf("unknown request type: %s", req.Type),
		}
	}
}
