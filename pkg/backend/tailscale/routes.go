package tailscale

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// RouteManager 管理 Tailscale 路由
type RouteManager struct {
	client     *SimpleClient
	advertised map[string]Route
	mu         sync.RWMutex
}

// Route 表示一个路由
type Route struct {
	Prefix     string    `json:"prefix"`
	Advertised bool      `json:"advertised"`
	Accepted   bool      `json:"accepted"`
	Enabled    bool      `json:"enabled"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated"`
}

// RouteStatus 路由状态信息
type RouteStatus struct {
	TotalRoutes      int              `json:"total_routes"`
	AdvertisedRoutes int              `json:"advertised_routes"`
	AcceptedRoutes   int              `json:"accepted_routes"`
	Routes           map[string]Route `json:"routes"`
}

// NewRouteManager 创建新的路由管理器
func NewRouteManager(client *SimpleClient) *RouteManager {
	return &RouteManager{
		client:     client,
		advertised: make(map[string]Route),
	}
}

// AdvertiseRoute 通告路由
func (rm *RouteManager) AdvertiseRoute(ctx context.Context, prefix string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 验证 CIDR 格式
	if err := rm.validateCIDR(prefix); err != nil {
		return fmt.Errorf("invalid CIDR format: %v", err)
	}

	// 检查是否已经通告
	if route, exists := rm.advertised[prefix]; exists && route.Advertised {
		return nil
	}

	// 获取当前所有要通告的路由
	routes := rm.getCurrentAdvertisedRoutes()
	routes = append(routes, prefix)

	// 通告路由
	if err := rm.client.AdvertiseRoute(ctx, routes...); err != nil {
		return fmt.Errorf("failed to advertise route %s: %v", prefix, err)
	}

	// 更新本地记录
	now := time.Now()
	route := Route{
		Prefix:     prefix,
		Advertised: true,
		Created:    now,
		Updated:    now,
	}

	if existingRoute, exists := rm.advertised[prefix]; exists {
		route.Created = existingRoute.Created
	}

	rm.advertised[prefix] = route

	return nil
}

// RemoveRoute 移除路由通告
func (rm *RouteManager) RemoveRoute(ctx context.Context, prefix string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 检查是否已通告
	if _, exists := rm.advertised[prefix]; !exists {
		return nil
	}

	// 获取当前所有路由，排除要删除的路由
	var remainingRoutes []string
	for route := range rm.advertised {
		if route != prefix {
			remainingRoutes = append(remainingRoutes, route)
		}
	}

	// 重新设置路由列表
	if err := rm.client.AdvertiseRoute(ctx, remainingRoutes...); err != nil {
		return fmt.Errorf("failed to update routes after removal: %v", err)
	}

	// 更新本地记录
	delete(rm.advertised, prefix)
	return nil
}

// RemoveRoutes 批量移除路由
func (rm *RouteManager) RemoveRoutes(ctx context.Context, prefixes []string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 获取要保留的路由
	toRemove := make(map[string]bool)
	for _, prefix := range prefixes {
		toRemove[prefix] = true
	}

	var remainingRoutes []string
	for route := range rm.advertised {
		if !toRemove[route] {
			remainingRoutes = append(remainingRoutes, route)
		}
	}

	// 重新设置路由列表
	if err := rm.client.AdvertiseRoute(ctx, remainingRoutes...); err != nil {
		return fmt.Errorf("failed to update routes after removal: %v", err)
	}

	// 更新本地记录
	for _, prefix := range prefixes {
		delete(rm.advertised, prefix)
	}

	return nil
}

// AcceptRoutes 接受路由
func (rm *RouteManager) AcceptRoutes(ctx context.Context) error {
	return rm.client.AcceptRoutes(ctx)
}

// GetAdvertisedRoutes 获取已通告的路由
func (rm *RouteManager) GetAdvertisedRoutes() []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	routes := make([]string, 0, len(rm.advertised))
	for route := range rm.advertised {
		routes = append(routes, route)
	}

	return routes
}

// getCurrentAdvertisedRoutes 获取当前已通告的路由（内部方法，需要持有锁）
func (rm *RouteManager) getCurrentAdvertisedRoutes() []string {
	routes := make([]string, 0, len(rm.advertised))
	for route, r := range rm.advertised {
		if r.Advertised {
			routes = append(routes, route)
		}
	}
	return routes
}

// IsRouteAdvertised 检查路由是否已通告
func (rm *RouteManager) IsRouteAdvertised(prefix string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	route, exists := rm.advertised[prefix]
	return exists && route.Advertised
}

// GetRouteStatus 获取路由状态
func (rm *RouteManager) GetRouteStatus() map[string]Route {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	status := make(map[string]Route)
	for prefix, route := range rm.advertised {
		status[prefix] = route
	}

	return status
}

// GetDetailedRouteStatus 获取详细路由状态
func (rm *RouteManager) GetDetailedRouteStatus(ctx context.Context) (*RouteStatus, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	status := &RouteStatus{
		Routes: make(map[string]Route),
	}

	// 获取当前Tailscale状态
	tsStatus, err := rm.client.GetStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tailscale status: %v", err)
	}

	// 统计路由信息
	for prefix, route := range rm.advertised {
		status.Routes[prefix] = route
		status.TotalRoutes++
		if route.Advertised {
			status.AdvertisedRoutes++
		}
	}

	// 从Tailscale状态获取实际启用的路由信息
	// 注意: 这里需要根据实际的状态结构调整
	if tsStatus != nil {
		// 可以在这里添加更多状态检查逻辑
	}

	return status, nil
}

// ValidateRoute 验证路由格式
func (rm *RouteManager) ValidateRoute(prefix string) error {
	return rm.validateCIDR(prefix)
}

// validateCIDR 验证CIDR格式
func (rm *RouteManager) validateCIDR(prefix string) error {
	// 使用netip包验证（更现代的方式）
	_, err := netip.ParsePrefix(prefix)
	if err != nil {
		// 兼容旧的net包格式
		_, _, err2 := net.ParseCIDR(prefix)
		if err2 != nil {
			return fmt.Errorf("invalid CIDR format %s: %v", prefix, err)
		}
	}
	return nil
}

// GetLocalPoolFromPodIP 从 Pod IP 推断本地池 CIDR
func (rm *RouteManager) GetLocalPoolFromPodIP(podIP string) string {
	// 解析Pod IP
	addr, err := netip.ParseAddr(podIP)
	if err != nil {
		// 兼容旧格式
		ip := net.ParseIP(podIP)
		if ip == nil {
			return ""
		}
		var ok bool
		addr, ok = netip.AddrFromSlice(ip)
		if !ok {
			return ""
		}
	}

	// 常见的本地池模式
	if addr.Is4() {
		// IPv4处理
		octets := addr.As4()

		// Kubernetes默认Pod CIDR: 10.244.x.y -> 10.244.x.0/24
		if octets[0] == 10 && octets[1] == 244 {
			return fmt.Sprintf("10.244.%d.0/24", octets[2])
		}

		// Docker默认网络: 172.17.x.y -> 172.17.0.0/16
		if octets[0] == 172 && octets[1] == 17 {
			return "172.17.0.0/16"
		}

		// 私有网络范围
		if octets[0] == 172 && octets[1] >= 16 && octets[1] <= 31 {
			return fmt.Sprintf("172.%d.0.0/16", octets[1])
		}

		// 本地网络: 192.168.x.y -> 192.168.x.0/24
		if octets[0] == 192 && octets[1] == 168 {
			return fmt.Sprintf("192.168.%d.0/24", octets[2])
		}

		// 10.x.x.x网络
		if octets[0] == 10 {
			// 尝试/24网络
			return fmt.Sprintf("10.%d.%d.0/24", octets[1], octets[2])
		}
	} else if addr.Is6() {
		// IPv6处理（简化）
		// 可以根据需要添加IPv6的本地池检测逻辑
		return ""
	}

	return ""
}

// EnsureLocalPoolRoute 确保本地池路由已通告
func (rm *RouteManager) EnsureLocalPoolRoute(ctx context.Context, podIP string) error {
	localPoolCIDR := rm.GetLocalPoolFromPodIP(podIP)
	if localPoolCIDR == "" {
		return fmt.Errorf("failed to determine local pool CIDR for pod IP %s", podIP)
	}

	// 检查是否已经通告
	if rm.IsRouteAdvertised(localPoolCIDR) {
		return nil
	}

	// 通告路由
	return rm.AdvertiseRoute(ctx, localPoolCIDR)
}

// SyncRoutes 同步路由状态（从Tailscale获取实际状态）
func (rm *RouteManager) SyncRoutes(ctx context.Context) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 获取Tailscale当前偏好设置
	prefs, err := rm.client.GetPrefs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get preferences: %v", err)
	}

	// 更新本地记录以反映实际状态
	actualRoutes := make(map[string]bool)
	if prefs.AdvertiseRoutes != nil {
		for _, route := range prefs.AdvertiseRoutes {
			actualRoutes[route.String()] = true
		}
	}

	now := time.Now()

	// 更新已通告路由的状态
	for prefix, route := range rm.advertised {
		if actualRoutes[prefix] {
			route.Advertised = true
			route.Updated = now
		} else {
			route.Advertised = false
			route.Updated = now
		}
		rm.advertised[prefix] = route
	}

	// 添加在Tailscale中但本地记录没有的路由
	for routeStr := range actualRoutes {
		if _, exists := rm.advertised[routeStr]; !exists {
			rm.advertised[routeStr] = Route{
				Prefix:     routeStr,
				Advertised: true,
				Created:    now,
				Updated:    now,
			}
		}
	}

	return nil
}

// ClearAllRoutes 清空所有路由通告
func (rm *RouteManager) ClearAllRoutes(ctx context.Context) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 清空路由通告
	if err := rm.client.AdvertiseRoute(ctx); err != nil {
		return fmt.Errorf("failed to clear routes: %v", err)
	}

	// 清空本地记录
	rm.advertised = make(map[string]Route)
	return nil
}

// GetRoutesByType 按类型获取路由
func (rm *RouteManager) GetRoutesByType() map[string][]string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := map[string][]string{
		"private":   []string{},
		"public":    []string{},
		"local":     []string{},
		"multicast": []string{},
	}

	for prefix := range rm.advertised {
		addr, err := netip.ParsePrefix(prefix)
		if err != nil {
			continue
		}

		if addr.Addr().IsPrivate() {
			result["private"] = append(result["private"], prefix)
		} else if addr.Addr().IsGlobalUnicast() {
			result["public"] = append(result["public"], prefix)
		} else if addr.Addr().IsLoopback() || addr.Addr().IsLinkLocalUnicast() {
			result["local"] = append(result["local"], prefix)
		} else if addr.Addr().IsMulticast() {
			result["multicast"] = append(result["multicast"], prefix)
		}
	}

	return result
}

// FilterRoutesByPattern 按模式过滤路由
func (rm *RouteManager) FilterRoutesByPattern(pattern string) []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var matched []string
	for prefix := range rm.advertised {
		if strings.Contains(prefix, pattern) {
			matched = append(matched, prefix)
		}
	}

	return matched
}

// AdvertiseRoutes 批量通告路由
func (rm *RouteManager) AdvertiseRoutes(ctx context.Context, prefixes []string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 验证所有路由格式
	for _, prefix := range prefixes {
		if err := rm.validateCIDR(prefix); err != nil {
			return fmt.Errorf("invalid CIDR format %s: %v", prefix, err)
		}
	}

	// 获取当前所有要通告的路由
	allRoutes := rm.getCurrentAdvertisedRoutes()

	// 添加新路由（去重）
	routeMap := make(map[string]bool)
	for _, route := range allRoutes {
		routeMap[route] = true
	}

	for _, prefix := range prefixes {
		if !routeMap[prefix] {
			allRoutes = append(allRoutes, prefix)
		}
	}

	// 通告所有路由
	if err := rm.client.AdvertiseRoute(ctx, allRoutes...); err != nil {
		return fmt.Errorf("failed to advertise routes: %v", err)
	}

	// 更新本地记录
	now := time.Now()
	for _, prefix := range prefixes {
		route := Route{
			Prefix:     prefix,
			Advertised: true,
			Created:    now,
			Updated:    now,
		}

		if existingRoute, exists := rm.advertised[prefix]; exists {
			route.Created = existingRoute.Created
		}

		rm.advertised[prefix] = route
	}

	return nil
}

// IsRoutesAccepted 检查是否接受路由
func (rm *RouteManager) IsRoutesAccepted(ctx context.Context) (bool, error) {
	prefs, err := rm.client.GetPrefs(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get preferences: %v", err)
	}
	if prefs == nil {
		return false, fmt.Errorf("preferences is nil")
	}
	return prefs.RouteAll, nil
}

// GetRouteCount 获取路由数量统计
func (rm *RouteManager) GetRouteCount() map[string]int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	counts := map[string]int{
		"total":      0,
		"advertised": 0,
		"private":    0,
		"public":     0,
	}

	for prefix, route := range rm.advertised {
		counts["total"]++
		if route.Advertised {
			counts["advertised"]++
		}

		addr, err := netip.ParsePrefix(prefix)
		if err != nil {
			continue
		}

		if addr.Addr().IsPrivate() {
			counts["private"]++
		} else if addr.Addr().IsGlobalUnicast() {
			counts["public"]++
		}
	}

	return counts
}

// IsRouteValid 检查路由是否有效
func (rm *RouteManager) IsRouteValid(prefix string) bool {
	return rm.validateCIDR(prefix) == nil
}

// GetRouteInfo 获取路由详细信息
func (rm *RouteManager) GetRouteInfo(prefix string) (*Route, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	route, exists := rm.advertised[prefix]
	return &route, exists
}

// UpdateRouteStatus 更新路由状态
func (rm *RouteManager) UpdateRouteStatus(prefix string, advertised, accepted, enabled bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if route, exists := rm.advertised[prefix]; exists {
		route.Advertised = advertised
		route.Accepted = accepted
		route.Enabled = enabled
		route.Updated = time.Now()
		rm.advertised[prefix] = route
	}
}

// GetRoutesByStatus 按状态获取路由
func (rm *RouteManager) GetRoutesByStatus() map[string][]string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := map[string][]string{
		"advertised": []string{},
		"accepted":   []string{},
		"enabled":    []string{},
		"inactive":   []string{},
	}

	for prefix, route := range rm.advertised {
		if route.Advertised {
			result["advertised"] = append(result["advertised"], prefix)
		}
		if route.Accepted {
			result["accepted"] = append(result["accepted"], prefix)
		}
		if route.Enabled {
			result["enabled"] = append(result["enabled"], prefix)
		}
		if !route.Advertised && !route.Accepted && !route.Enabled {
			result["inactive"] = append(result["inactive"], prefix)
		}
	}

	return result
}

// GetRouteStatistics 获取路由统计信息
func (rm *RouteManager) GetRouteStatistics() map[string]interface{} {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	stats := map[string]interface{}{
		"total_routes":      len(rm.advertised),
		"advertised_routes": 0,
		"accepted_routes":   0,
		"enabled_routes":    0,
		"route_types":       make(map[string]int),
		"recent_updates":    0,
	}

	now := time.Now()
	recentThreshold := now.Add(-5 * time.Minute)

	for prefix, route := range rm.advertised {
		if route.Advertised {
			stats["advertised_routes"] = stats["advertised_routes"].(int) + 1
		}
		if route.Accepted {
			stats["accepted_routes"] = stats["accepted_routes"].(int) + 1
		}
		if route.Enabled {
			stats["enabled_routes"] = stats["enabled_routes"].(int) + 1
		}
		if route.Updated.After(recentThreshold) {
			stats["recent_updates"] = stats["recent_updates"].(int) + 1
		}

		// 按类型统计
		addr, err := netip.ParsePrefix(prefix)
		if err == nil {
			routeType := "unknown"
			if addr.Addr().IsPrivate() {
				routeType = "private"
			} else if addr.Addr().IsGlobalUnicast() {
				routeType = "public"
			} else if addr.Addr().IsLoopback() || addr.Addr().IsLinkLocalUnicast() {
				routeType = "local"
			} else if addr.Addr().IsMulticast() {
				routeType = "multicast"
			}
			stats["route_types"].(map[string]int)[routeType]++
		}
	}

	return stats
}

// ValidateAllRoutes 验证所有路由
func (rm *RouteManager) ValidateAllRoutes() []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var invalidRoutes []string
	for prefix := range rm.advertised {
		if err := rm.validateCIDR(prefix); err != nil {
			invalidRoutes = append(invalidRoutes, prefix)
		}
	}

	return invalidRoutes
}

// GetRoutesByCreationTime 按创建时间获取路由
func (rm *RouteManager) GetRoutesByCreationTime(since time.Time) []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var routes []string
	for prefix, route := range rm.advertised {
		if route.Created.After(since) {
			routes = append(routes, prefix)
		}
	}

	return routes
}

// GetRoutesByUpdateTime 按更新时间获取路由
func (rm *RouteManager) GetRoutesByUpdateTime(since time.Time) []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var routes []string
	for prefix, route := range rm.advertised {
		if route.Updated.After(since) {
			routes = append(routes, prefix)
		}
	}

	return routes
}

// BackupRoutes 备份路由状态
func (rm *RouteManager) BackupRoutes() map[string]Route {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	backup := make(map[string]Route)
	for prefix, route := range rm.advertised {
		backup[prefix] = route
	}

	return backup
}

// RestoreRoutes 恢复路由状态
func (rm *RouteManager) RestoreRoutes(backup map[string]Route) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.advertised = make(map[string]Route)
	for prefix, route := range backup {
		rm.advertised[prefix] = route
	}
}

// GetRouteHistory 获取路由历史记录
func (rm *RouteManager) GetRouteHistory(prefix string) []Route {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// 这里可以实现更复杂的历史记录逻辑
	// 目前只返回当前状态
	if route, exists := rm.advertised[prefix]; exists {
		return []Route{route}
	}

	return nil
}

// CleanupOldRoutes 清理旧的路由记录
func (rm *RouteManager) CleanupOldRoutes(olderThan time.Duration) int {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for prefix, route := range rm.advertised {
		if route.Updated.Before(cutoff) && !route.Advertised {
			delete(rm.advertised, prefix)
			removed++
		}
	}

	return removed
}
