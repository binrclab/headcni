package cni

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client 是 CNI 客户端
type Client struct {
	socketPath string
	httpClient *http.Client
}

// Request 是 CNI 请求
type Request struct {
	Type        string `json:"type"` // "allocate", "release", "status"
	Namespace   string `json:"namespace"`
	PodName     string `json:"pod_name"`
	ContainerID string `json:"container_id"`
	PodIP       string `json:"pod_ip,omitempty"`
}

// Response 是 CNI 响应
type Response struct {
	Success bool        `json:"success"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// NewClient 创建新的 CNI 客户端
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// AllocateIP 分配 IP 地址
func (c *Client) AllocateIP(namespace, podName, containerID string) (string, error) {
	req := &Request{
		Type:        "allocate",
		Namespace:   namespace,
		PodName:     podName,
		ContainerID: containerID,
	}

	resp, err := c.SendRequest(req)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("allocation failed: %s", resp.Error)
	}

	// 解析响应数据
	if data, ok := resp.Data.(map[string]interface{}); ok {
		if ip, ok := data["ip"].(string); ok {
			return ip, nil
		}
	}

	return "", fmt.Errorf("invalid response data")
}

// ReleaseIP 释放 IP 地址
func (c *Client) ReleaseIP(namespace, podName, containerID string) error {
	req := &Request{
		Type:        "release",
		Namespace:   namespace,
		PodName:     podName,
		ContainerID: containerID,
	}

	resp, err := c.SendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("release failed: %s", resp.Error)
	}

	return nil
}

// GetPodStatus 获取 Pod 状态
func (c *Client) GetPodStatus(namespace, podName, containerID string) (*Response, error) {
	req := &Request{
		Type:        "status",
		Namespace:   namespace,
		PodName:     podName,
		ContainerID: containerID,
	}

	return c.SendRequest(req)
}

// SendRequest 发送请求到 Daemon
func (c *Client) SendRequest(req *Request) (*Response, error) {
	// 构造请求体
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	// 创建 HTTP 请求
	httpReq, err := http.NewRequest("POST", "http://unix/cni", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// 解析响应
	var cniResp Response
	if err := json.NewDecoder(resp.Body).Decode(&cniResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &cniResp, nil
}
