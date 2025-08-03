package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Client struct {
	*kubernetes.Clientset
}

func New() *Client {
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount"); err != nil {
		return LocalAuth()
	}
	return ClusterAuth()
}

func ClusterAuth() *Client {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	return &Client{kubernetes.NewForConfigOrDie(config)}
}

func LocalAuth() *Client {
	var kubeconfig string

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}
	return &Client{kubernetes.NewForConfigOrDie(config)}
}

func (c *Client) GetNode(name string) (*Node, error) {
	node, err := c.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, fmt.Errorf("error getting node: %w", err)
	}
	return NewNode(node), nil
}

func (c *Client) GetNodes() ([]*Node, error) {
	var nodes []*Node

	nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}
	if nodeList == nil {
		return nil, fmt.Errorf("error getting nodes")
	}

	for _, node := range nodeList.Items {
		nodes = append(nodes, NewNode(&node))
	}

	return nodes, err
}

func (c *Client) GetNodesByRole(role string) ([]*Node, error) {
	if role == "" {
		return c.GetNodes()
	}

	var nodes []*Node
	nodeList, err := c.CoreV1().Nodes().List(
		context.Background(), metav1.ListOptions{LabelSelector: "node-role.kubernetes.io/" + role})
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}
	if nodeList == nil {
		return nil, fmt.Errorf("error getting nodes")
	}

	for _, node := range nodeList.Items {
		nodes = append(nodes, NewNode(&node))
	}

	return nodes, err
}

func (c *Client) GetPods(namespace string, opts metav1.ListOptions) ([]*Pod, error) {
	var pods []*Pod

	podList, err := c.CoreV1().Pods(namespace).List(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}
	if podList == nil {
		return nil, fmt.Errorf("error getting pods")
	}
	for _, pod := range podList.Items {
		pods = append(pods, NewPod(&pod))
	}

	return pods, nil
}
