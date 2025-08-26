//go:build !linux
// +build !linux

package health

// 重新导出 netlink 常量（非 Linux 平台）
const (
	FAMILY_ALL = 0
	OperDown   = 2
)

// 重新导出 netlink 类型（非 Linux 平台）
type Link interface {
	Attrs() *LinkAttrs
	Type() string
}

type Route struct {
	LinkIndex int
}

type LinkAttrs struct {
	Name      string
	OperState int
}

// RouteList 获取路由列表（非 Linux 存根实现）
func RouteList(link Link, family int) ([]Route, error) {
	return []Route{}, nil
}

// LinkByIndex 根据索引获取网络接口（非 Linux 存根实现）
func LinkByIndex(index int) (Link, error) {
	return nil, nil
}

// LinkList 获取所有网络接口（非 Linux 存根实现）
func LinkList() ([]Link, error) {
	return []Link{}, nil
}

// LinkDel 删除网络接口（非 Linux 存根实现）
func LinkDel(link Link) error {
	return nil
}
