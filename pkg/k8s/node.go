package k8s

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// GetCurrentNode 获取当前节点信息
func (c *Client) GetCurrentNode() (*coreV1.Node, error) {
	nodeName, err := c.GetCurrentNodeName()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get current node name")
	}

	return c.GetNodeByID(nodeName)
}

// GetNodeByID 根据节点ID获取节点
func (c *Client) GetNodeByID(nodeID string) (*coreV1.Node, error) {
	c.mu.RLock()
	clientset := c.clientset
	c.mu.RUnlock()

	if clientset == nil {
		return nil, errors.New("client not connected")
	}

	node, err := clientset.CoreV1().Nodes().Get(context.TODO(), nodeID, metaV1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get node %s", nodeID)
	}
	return node, nil
}

// GetAllNodes 获取所有节点
func (c *Client) GetAllNodes() ([]*coreV1.Node, error) {
	c.mu.RLock()
	clientset := c.clientset
	c.mu.RUnlock()

	if clientset == nil {
		return nil, errors.New("client not connected")
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metaV1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list nodes")
	}

	var nodeList []*coreV1.Node
	for i := range nodes.Items {
		nodeList = append(nodeList, &nodes.Items[i])
	}

	return nodeList, nil
}

// PatchNode 更新节点信息
func (c *Client) PatchNode(oldNode, newNode *coreV1.Node) error {
	c.mu.RLock()
	clientset := c.clientset
	c.mu.RUnlock()

	if clientset == nil {
		return errors.New("client not connected")
	}

	oldData, err := json.Marshal(oldNode)
	if err != nil {
		return errors.Wrap(err, "failed to marshal old node")
	}

	newData, err := json.Marshal(newNode)
	if err != nil {
		return errors.Wrap(err, "failed to marshal new node")
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, coreV1.Node{})
	if err != nil {
		return errors.Wrap(err, "failed to create merge patch")
	}

	if _, err = clientset.CoreV1().Nodes().Patch(context.TODO(), oldNode.Name, types.StrategicMergePatchType,
		patchBytes, metaV1.PatchOptions{}); err != nil {
		return errors.Wrapf(err, "failed to patch node %s", oldNode.Name)
	}

	return nil
}

// 向后兼容的全局函数
func GetCurrentNode() (*coreV1.Node, error) {
	if globalClient == nil {
		return nil, errors.New("global client not initialized")
	}
	return globalClient.GetCurrentNode()
}

func GetNodeByID(nodeID string) (*coreV1.Node, error) {
	if globalClient == nil {
		return nil, errors.New("global client not initialized")
	}
	return globalClient.GetNodeByID(nodeID)
}

func GetAllNodes() ([]*coreV1.Node, error) {
	if globalClient == nil {
		return nil, errors.New("global client not initialized")
	}
	return globalClient.GetAllNodes()
}

func PatchNode(oldNode, newNode *coreV1.Node) error {
	if globalClient == nil {
		return errors.New("global client not initialized")
	}
	return globalClient.PatchNode(oldNode, newNode)
}
