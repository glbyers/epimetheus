package k8s

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"strings"
)

type SimpleNode struct {
	Name    string   `json:"name"`
	Address string   `json:"address"`
	Roles   []string `json:"roles"`
}

type Node struct {
	SimpleNode
	*corev1.Node
}

type NodeStatus struct {
	Node   *Node    `json:"node"`
	Errors []string `json:"errors"`
}

func NewNode(node *corev1.Node) *Node {
	n := Node{Node: node}

	// Name & Address
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			n.Address = addr.Address
		} else if addr.Type == corev1.NodeHostName {
			n.Name = addr.Address
		}
	}

	// Roles
	for label := range node.Labels {
		if strings.HasPrefix(label, "node-role.kubernetes.io/") {
			parts := strings.Split(label, "/")
			n.Roles = append(n.Roles, parts[len(parts)-1])
		}
	}

	return &n
}

func (n *Node) GetAddress(addrType corev1.NodeAddressType) (address string) {
	for _, addr := range n.Node.Status.Addresses {
		if addr.Type == addrType {
			address = addr.Address
		}
	}
	return
}

func (n *Node) Status() (status NodeStatus) {
	status.Node = n
	for _, cond := range n.Node.Status.Conditions {
		// All conditions except NodeReady should be false
		if cond.Type != corev1.NodeReady && cond.Status != corev1.ConditionFalse {
			status.Errors = append(status.Errors, fmt.Sprintf("%v: %s", cond.Type, cond.Message))
		} else if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
			status.Errors = append(status.Errors, fmt.Sprintf("%v: %s", cond.Type, cond.Message))
		}
	}
	return
}
