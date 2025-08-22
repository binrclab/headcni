// pkg/policy/manager.go
package policy

import (
	"context"
	"fmt"
	"net"
	"sync"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type NetworkPolicyManager struct {
	client      kubernetes.Interface
	nodeName    string
	policies    map[string]*NetworkPolicyRule
	policyMutex sync.RWMutex
}

type NetworkPolicyRule struct {
	Namespace   string
	Name        string
	PodSelector metav1.LabelSelector
	Ingress     []networkingv1.NetworkPolicyIngressRule
	Egress      []networkingv1.NetworkPolicyEgressRule
}

func NewNetworkPolicyManager(client kubernetes.Interface, nodeName string) *NetworkPolicyManager {
	return &NetworkPolicyManager{
		client:   client,
		nodeName: nodeName,
		policies: make(map[string]*NetworkPolicyRule),
	}
}

func (npm *NetworkPolicyManager) SyncPolicies(ctx context.Context) error {
	policies, err := npm.client.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list network policies: %v", err)
	}

	npm.policyMutex.Lock()
	defer npm.policyMutex.Unlock()

	// 清空现有策略
	npm.policies = make(map[string]*NetworkPolicyRule)

	// 加载新策略
	for _, policy := range policies.Items {
		key := fmt.Sprintf("%s/%s", policy.Namespace, policy.Name)
		rule := &NetworkPolicyRule{
			Namespace:   policy.Namespace,
			Name:        policy.Name,
			PodSelector: policy.Spec.PodSelector,
			Ingress:     policy.Spec.Ingress,
			Egress:      policy.Spec.Egress,
		}
		npm.policies[key] = rule

		klog.V(4).Infof("Loaded network policy: %s", key)
	}

	return nil
}

func (npm *NetworkPolicyManager) GetPoliciesForPod(namespace, podName string, labels map[string]string) []*NetworkPolicyRule {
	npm.policyMutex.RLock()
	defer npm.policyMutex.RUnlock()

	var applicable []*NetworkPolicyRule

	for _, policy := range npm.policies {
		// 只考虑同一命名空间的策略
		if policy.Namespace != namespace {
			continue
		}

		// 检查 Pod 选择器
		if npm.matchesLabelSelector(labels, &policy.PodSelector) {
			applicable = append(applicable, policy)
		}
	}

	return applicable
}

func (npm *NetworkPolicyManager) matchesLabelSelector(podLabels map[string]string, selector *metav1.LabelSelector) bool {
	// 实现标签选择器匹配逻辑
	if selector == nil || len(selector.MatchLabels) == 0 {
		return true // 空选择器匹配所有
	}

	for key, value := range selector.MatchLabels {
		if podLabels[key] != value {
			return false
		}
	}

	// TODO: 实现 MatchExpressions 支持

	return true
}

// 在 Pod 创建时应用网络策略
func (npm *NetworkPolicyManager) ApplyPoliciesForPod(podIP net.IP, namespace, podName string, labels map[string]string) error {
	policies := npm.GetPoliciesForPod(namespace, podName, labels)

	if len(policies) == 0 {
		klog.V(4).Infof("No network policies apply to pod %s/%s", namespace, podName)
		return nil
	}

	klog.Infof("Applying %d network policies to pod %s/%s", len(policies), namespace, podName)

	// 将策略转换为 iptables 规则（简化版）
	for _, policy := range policies {
		if err := npm.applyPolicyRules(podIP, policy); err != nil {
			klog.Errorf("Failed to apply policy %s/%s: %v", policy.Namespace, policy.Name, err)
		}
	}

	return nil
}

func (npm *NetworkPolicyManager) applyPolicyRules(podIP net.IP, policy *NetworkPolicyRule) error {
	// 这里应该实现具体的策略应用逻辑
	// 可以使用 iptables、eBPF 或其他机制

	klog.V(4).Infof("Applied policy %s/%s to pod IP %s",
		policy.Namespace, policy.Name, podIP.String())

	return nil
}
