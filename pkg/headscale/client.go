package headscale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client 是 Headscale 客户端
type Client struct {
	baseURL    string
	httpClient *http.Client
	authKey    string
}

// API 响应结构体
type ListApiKeysResponse struct {
	ApiKeys []ApiKey `json:"apiKeys"`
}

type ApiKey struct {
	ID         string    `json:"id"`
	Prefix     string    `json:"prefix"`
	Expiration time.Time `json:"expiration"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeen   time.Time `json:"lastSeen"`
}

type CreateApiKeyRequest struct {
	Expiration time.Time `json:"expiration,omitempty"`
}

type CreateApiKeyResponse struct {
	ApiKey string `json:"apiKey"`
}

type ExpireApiKeyRequest struct {
	Prefix string `json:"prefix"`
}

type ListNodesResponse struct {
	Nodes []Node `json:"nodes"`
}

type Node struct {
	ID             string     `json:"id"`
	MachineKey     string     `json:"machineKey"`
	NodeKey        string     `json:"nodeKey"`
	DiscoKey       string     `json:"discoKey"`
	IPAddresses    []string   `json:"ipAddresses"`
	Name           string     `json:"name"`
	User           User       `json:"user"`
	LastSeen       time.Time  `json:"lastSeen"`
	Expiry         time.Time  `json:"expiry"`
	PreAuthKey     PreAuthKey `json:"preAuthKey"`
	CreatedAt      time.Time  `json:"createdAt"`
	RegisterMethod string     `json:"registerMethod"`
	ForcedTags     []string   `json:"forcedTags"`
	InvalidTags    []string   `json:"invalidTags"`
	ValidTags      []string   `json:"validTags"`
	GivenName      string     `json:"givenName"`
	Online         bool       `json:"online"`
}

type User struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	CreatedAt     time.Time `json:"createdAt"`
	DisplayName   string    `json:"displayName"`
	Email         string    `json:"email"`
	ProviderID    string    `json:"providerId"`
	Provider      string    `json:"provider"`
	ProfilePicURL string    `json:"profilePicUrl"`
}

type PreAuthKey struct {
	User       string    `json:"user"`
	ID         string    `json:"id"`
	Key        string    `json:"key"`
	Reusable   bool      `json:"reusable"`
	Ephemeral  bool      `json:"ephemeral"`
	Used       bool      `json:"used"`
	Expiration time.Time `json:"expiration"`
	CreatedAt  time.Time `json:"createdAt"`
	AclTags    []string  `json:"aclTags"`
}

type DebugCreateNodeRequest struct {
	User   string   `json:"user"`
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	Routes []string `json:"routes"`
}

type DebugCreateNodeResponse struct {
	Node Node `json:"node"`
}

type RegisterNodeResponse struct {
	Node Node `json:"node"`
}

type GetNodeResponse struct {
	Node Node `json:"node"`
}

type GetRoutesResponse struct {
	Routes []Route `json:"routes"`
}

type Route struct {
	ID         string    `json:"id"`
	Node       Node      `json:"node"`
	Prefix     string    `json:"prefix"`
	Advertised bool      `json:"advertised"`
	Enabled    bool      `json:"enabled"`
	IsPrimary  bool      `json:"isPrimary"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	DeletedAt  time.Time `json:"deletedAt"`
}

type GetNodeRoutesResponse struct {
	Routes []Route `json:"routes"`
}

type SetTagsRequest struct {
	Tags []string `json:"tags"`
}

type SetTagsResponse struct {
	Node Node `json:"node"`
}

type MoveNodeRequest struct {
	User string `json:"user"`
}

type MoveNodeResponse struct {
	Node Node `json:"node"`
}

type CreatePreAuthKeyRequest struct {
	User       string    `json:"user"`
	Reusable   bool      `json:"reusable"`
	Ephemeral  bool      `json:"ephemeral"`
	Expiration time.Time `json:"expiration,omitempty"`
	AclTags    []string  `json:"aclTags,omitempty"`
}

type CreatePreAuthKeyResponse struct {
	PreAuthKey PreAuthKey `json:"preAuthKey"`
}

type ExpirePreAuthKeyRequest struct {
	User string `json:"user"`
	Key  string `json:"key"`
}

type ListPreAuthKeysResponse struct {
	PreAuthKeys []PreAuthKey `json:"preAuthKeys"`
}

type ListUsersResponse struct {
	Users []User `json:"users"`
}

type CreateUserRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	PictureURL  string `json:"pictureUrl,omitempty"`
}

type CreateUserResponse struct {
	User User `json:"user"`
}

type GetPolicyResponse struct {
	Policy    string    `json:"policy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SetPolicyRequest struct {
	Policy string `json:"policy"`
}

type SetPolicyResponse struct {
	Policy    string    `json:"policy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// NewClient 创建新的 Headscale 客户端
func NewClient(baseURL, authKey string) (*Client, error) {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		authKey: authKey,
	}, nil
}

// Ping 发送 ping 请求到 Headscale
func (c *Client) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/v1/status", c.baseURL), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to ping headscale: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("headscale ping failed with status: %d", resp.StatusCode)
	}

	return nil
}

// 通用请求方法
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("%s%s", c.baseURL, path), bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// 添加认证头
	if c.authKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.authKey)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %v", err)
		}
	}

	return nil
}

// ==================== API Key 管理 ====================

// ListApiKeys 列出所有 API Key
func (c *Client) ListApiKeys(ctx context.Context) (*ListApiKeysResponse, error) {
	var result ListApiKeysResponse
	err := c.doRequest(ctx, "GET", "/api/v1/apikey", nil, &result)
	return &result, err
}

// CreateApiKey 创建新的 API Key
func (c *Client) CreateApiKey(ctx context.Context, req *CreateApiKeyRequest) (*CreateApiKeyResponse, error) {
	var result CreateApiKeyResponse
	err := c.doRequest(ctx, "POST", "/api/v1/apikey", req, &result)
	return &result, err
}

// ExpireApiKey 使 API Key 过期
func (c *Client) ExpireApiKey(ctx context.Context, prefix string) error {
	req := &ExpireApiKeyRequest{Prefix: prefix}
	return c.doRequest(ctx, "POST", "/api/v1/apikey/expire", req, nil)
}

// DeleteApiKey 删除 API Key
func (c *Client) DeleteApiKey(ctx context.Context, prefix string) error {
	return c.doRequest(ctx, "DELETE", fmt.Sprintf("/api/v1/apikey/%s", prefix), nil, nil)
}

// CheckApiKeyHealth 检查 API Key 是否有效
func (c *Client) CheckApiKeyHealth(ctx context.Context) error {
	// 尝试获取 API Key 列表来验证认证
	_, err := c.ListApiKeys(ctx)
	return err
}

// ==================== Node 管理 ====================

// ListNodes 列出所有节点
func (c *Client) ListNodes(ctx context.Context, user string) (*ListNodesResponse, error) {
	path := "/api/v1/node"
	if user != "" {
		path += "?user=" + user
	}

	var result ListNodesResponse
	err := c.doRequest(ctx, "GET", path, nil, &result)
	return &result, err
}

// GetNode 获取特定节点信息
func (c *Client) GetNode(ctx context.Context, nodeID string) (*GetNodeResponse, error) {
	var result GetNodeResponse
	err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/v1/node/%s", nodeID), nil, &result)
	return &result, err
}

// DeleteNode 删除节点
func (c *Client) DeleteNode(ctx context.Context, nodeID string) error {
	return c.doRequest(ctx, "DELETE", fmt.Sprintf("/api/v1/node/%s", nodeID), nil, nil)
}

// ExpireNode 使节点过期
func (c *Client) ExpireNode(ctx context.Context, nodeID string) (*GetNodeResponse, error) {
	var result GetNodeResponse
	err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/node/%s/expire", nodeID), nil, &result)
	return &result, err
}

// RenameNode 重命名节点
func (c *Client) RenameNode(ctx context.Context, nodeID, newName string) (*GetNodeResponse, error) {
	var result GetNodeResponse
	err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/node/%s/rename/%s", nodeID, newName), nil, &result)
	return &result, err
}

// DebugCreateNode 调试创建节点
func (c *Client) DebugCreateNode(ctx context.Context, req *DebugCreateNodeRequest) (*DebugCreateNodeResponse, error) {
	var result DebugCreateNodeResponse
	err := c.doRequest(ctx, "POST", "/api/v1/debug/node", req, &result)
	return &result, err
}

// RegisterNode 注册节点
func (c *Client) RegisterNode(ctx context.Context, user, key string) (*RegisterNodeResponse, error) {
	path := fmt.Sprintf("/api/v1/node/register?user=%s&key=%s", user, key)
	var result RegisterNodeResponse
	err := c.doRequest(ctx, "POST", path, nil, &result)
	return &result, err
}

// GetNodeRoutes 获取节点的路由
func (c *Client) GetNodeRoutes(ctx context.Context, nodeID string) (*GetNodeRoutesResponse, error) {
	var result GetNodeRoutesResponse
	err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/v1/node/%s/routes", nodeID), nil, &result)
	return &result, err
}

// SetNodeTags 设置节点标签
func (c *Client) SetNodeTags(ctx context.Context, nodeID string, tags []string) (*SetTagsResponse, error) {
	req := &SetTagsRequest{Tags: tags}
	var result SetTagsResponse
	err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/node/%s/tags", nodeID), req, &result)
	return &result, err
}

// MoveNode 移动节点到其他用户
func (c *Client) MoveNode(ctx context.Context, nodeID, user string) (*MoveNodeResponse, error) {
	req := &MoveNodeRequest{User: user}
	var result MoveNodeResponse
	err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/node/%s/user", nodeID), req, &result)
	return &result, err
}

// ==================== PreAuthKey 管理 ====================

// ListPreAuthKeys 列出预授权密钥
func (c *Client) ListPreAuthKeys(ctx context.Context, user string) (*ListPreAuthKeysResponse, error) {
	path := "/api/v1/preauthkey"
	if user != "" {
		path += "?user=" + user
	}

	var result ListPreAuthKeysResponse
	err := c.doRequest(ctx, "GET", path, nil, &result)
	return &result, err
}

// CreatePreAuthKey 创建预授权密钥
func (c *Client) CreatePreAuthKey(ctx context.Context, req *CreatePreAuthKeyRequest) (*CreatePreAuthKeyResponse, error) {
	var result CreatePreAuthKeyResponse
	err := c.doRequest(ctx, "POST", "/api/v1/preauthkey", req, &result)
	return &result, err
}

// ExpirePreAuthKey 使预授权密钥过期
func (c *Client) ExpirePreAuthKey(ctx context.Context, user, key string) error {
	req := &ExpirePreAuthKeyRequest{User: user, Key: key}
	return c.doRequest(ctx, "POST", "/api/v1/preauthkey/expire", req, nil)
}

// ==================== User 管理 ====================

// ListUsers 列出所有用户
func (c *Client) ListUsers(ctx context.Context, id, name, email string) (*ListUsersResponse, error) {
	path := "/api/v1/user"
	params := make([]string, 0)
	if id != "" {
		params = append(params, "id="+id)
	}
	if name != "" {
		params = append(params, "name="+name)
	}
	if email != "" {
		params = append(params, "email="+email)
	}

	if len(params) > 0 {
		path += "?" + params[0]
		for i := 1; i < len(params); i++ {
			path += "&" + params[i]
		}
	}

	var result ListUsersResponse
	err := c.doRequest(ctx, "GET", path, nil, &result)
	return &result, err
}

// CreateUser 创建用户
func (c *Client) CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
	var result CreateUserResponse
	err := c.doRequest(ctx, "POST", "/api/v1/user", req, &result)
	return &result, err
}

// DeleteUser 删除用户
func (c *Client) DeleteUser(ctx context.Context, userID string) error {
	return c.doRequest(ctx, "DELETE", fmt.Sprintf("/api/v1/user/%s", userID), nil, nil)
}

// RenameUser 重命名用户
func (c *Client) RenameUser(ctx context.Context, oldID, newName string) (*CreateUserResponse, error) {
	var result CreateUserResponse
	err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/user/%s/rename/%s", oldID, newName), nil, &result)
	return &result, err
}

// ==================== Route 管理 ====================

// GetRoutes 获取所有路由
func (c *Client) GetRoutes(ctx context.Context) (*GetRoutesResponse, error) {
	var result GetRoutesResponse
	err := c.doRequest(ctx, "GET", "/api/v1/routes", nil, &result)
	return &result, err
}

// DeleteRoute 删除路由
func (c *Client) DeleteRoute(ctx context.Context, routeID string) error {
	return c.doRequest(ctx, "DELETE", fmt.Sprintf("/api/v1/routes/%s", routeID), nil, nil)
}

// EnableRoute 启用路由
func (c *Client) EnableRoute(ctx context.Context, routeID string) error {
	return c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/routes/%s/enable", routeID), nil, nil)
}

// DisableRoute 禁用路由
func (c *Client) DisableRoute(ctx context.Context, routeID string) error {
	return c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/routes/%s/disable", routeID), nil, nil)
}

// ==================== Policy 管理 ====================

// GetPolicy 获取策略
func (c *Client) GetPolicy(ctx context.Context) (*GetPolicyResponse, error) {
	var result GetPolicyResponse
	err := c.doRequest(ctx, "GET", "/api/v1/policy", nil, &result)
	return &result, err
}

// SetPolicy 设置策略
func (c *Client) SetPolicy(ctx context.Context, policy string) (*SetPolicyResponse, error) {
	req := &SetPolicyRequest{Policy: policy}
	var result SetPolicyResponse
	err := c.doRequest(ctx, "PUT", "/api/v1/policy", req, &result)
	return &result, err
}

// ==================== 高级功能 ====================

// RequestRoute 请求 Headscale 允许特定 IP 的路由
func (c *Client) RequestRoute(podIP string) error {
	// 这里实现向 Headscale 请求路由的逻辑
	// 可以通过创建节点并设置路由来实现
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 创建预授权密钥
	preAuthReq := &CreatePreAuthKeyRequest{
		User:      "headcni",
		Reusable:  true,
		Ephemeral: false,
	}

	preAuthResp, err := c.CreatePreAuthKey(ctx, preAuthReq)
	if err != nil {
		return fmt.Errorf("failed to create pre-auth key: %v", err)
	}

	// 2. 创建节点
	nodeReq := &DebugCreateNodeRequest{
		User:   "headcni",
		Key:    preAuthResp.PreAuthKey.Key,
		Name:   fmt.Sprintf("headcni-pod-%s", podIP),
		Routes: []string{podIP + "/32"},
	}

	_, err = c.DebugCreateNode(ctx, nodeReq)
	if err != nil {
		return fmt.Errorf("failed to create node: %v", err)
	}

	return nil
}

// ValidateNodeKey 验证节点密钥是否有效
func (c *Client) ValidateNodeKey(ctx context.Context, nodeKey string) (bool, error) {
	// 通过尝试获取节点信息来验证密钥
	nodes, err := c.ListNodes(ctx, "")
	if err != nil {
		return false, err
	}

	for _, node := range nodes.Nodes {
		if node.NodeKey == nodeKey {
			return true, nil
		}
	}

	return false, nil
}

// GetNodeByKey 通过节点密钥获取节点信息
func (c *Client) GetNodeByKey(ctx context.Context, nodeKey string) (*Node, error) {
	nodes, err := c.ListNodes(ctx, "")
	if err != nil {
		return nil, err
	}

	for _, node := range nodes.Nodes {
		if node.NodeKey == nodeKey {
			return &node, nil
		}
	}

	return nil, fmt.Errorf("node with key %s not found", nodeKey)
}

// CleanupExpiredNodes 清理过期的节点
func (c *Client) CleanupExpiredNodes(ctx context.Context) error {
	nodes, err := c.ListNodes(ctx, "")
	if err != nil {
		return err
	}

	for _, node := range nodes.Nodes {
		if !node.Expiry.IsZero() && time.Now().After(node.Expiry) {
			if err := c.DeleteNode(ctx, node.ID); err != nil {
				return fmt.Errorf("failed to delete expired node %s: %v", node.ID, err)
			}
		}
	}

	return nil
}
