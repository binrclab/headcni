package cni

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	// 验证并清理 socket 路径
	cleanPath := s.validateAndCleanSocketPath()
	if cleanPath == "" {
		return fmt.Errorf("socket path is empty or invalid")
	}

	// 删除 socket 文件
	if err := os.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove socket file: %v", err)
	}
	return nil
}

// prepareSocketDirectory 准备 socket 目录
func (s *Server) prepareSocketDirectory() error {
	// 验证并清理 socket 路径
	cleanPath := s.validateAndCleanSocketPath()
	if cleanPath == "" {
		return fmt.Errorf("socket path is empty or invalid")
	}

	// 创建 socket 目录
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %v", err)
	}

	// 删除已存在的 socket 文件
	if err := os.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %v", err)
	}

	return nil
}

// validateAndCleanSocketPath 验证并清理 socket 路径，确保使用正确的 Unix socket 格式
func (s *Server) validateAndCleanSocketPath() string {
	path := s.socketPath

	// 如果路径为空，返回错误
	if path == "" {
		return ""
	}

	// 移除可能的错误协议前缀
	path = strings.TrimPrefix(path, "tcp://")
	path = strings.TrimPrefix(path, "http://")
	path = strings.TrimPrefix(path, "https://")
	path = strings.TrimPrefix(path, "unix://")

	// 确保路径以 / 开头（绝对路径）
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// 记录清理后的路径
	if path != s.socketPath {
		logging.Warnf("Socket path cleaned from '%s' to '%s'", s.socketPath, path)
	}

	return path
}

// createAndStartHTTPServer 创建并启动 HTTP 服务器
func (s *Server) createAndStartHTTPServer() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/cni", s.handleCNIRequest)

	// 验证并清理 socket 路径
	cleanPath := s.validateAndCleanSocketPath()
	if cleanPath == "" {
		return fmt.Errorf("socket path is empty or invalid")
	}

	// 创建 Unix socket 监听器
	listener, err := net.Listen("unix", cleanPath)
	if err != nil {
		return fmt.Errorf("failed to create Unix socket listener: %v", err)
	}

	s.server = &http.Server{
		Handler: mux,
	}

	// 启动服务器
	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logging.Errorf("CNI server error: %v", err)
		}
	}()

	logging.Infof("CNI server started on Unix socket: %s", cleanPath)
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
