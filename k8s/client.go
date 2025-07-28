package k8s

import (
	"context"
	"flag"
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
	conn *kubernetes.Clientset
}

func Connect() (client *Client) {
	_, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount")
	if err != nil {
		client = LocalAuth()
	} else {
		client = ClusterAuth()
	}
	return
}

func ClusterAuth() *Client {
	var client Client

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	client.conn = kubernetes.NewForConfigOrDie(config)

	return &client
}

func LocalAuth() *Client {
	var client Client
	var kubeconfig *string

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	clientset := kubernetes.NewForConfigOrDie(config)
	client.conn = clientset

	return &client
}

func (c *Client) GetNode(name string) (*Node, error) {
	node, err := c.conn.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, err
	}
	return NewNode(node), nil
}

func (c *Client) GetNodes() ([]*Node, error) {
	var nodes []*Node

	nodeList, err := c.conn.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, err
	}

	for _, node := range nodeList.Items {
		nodes = append(nodes, NewNode(&node))
	}

	return nodes, nil
}

func (c *Client) GetNodesByRole(role string) ([]*Node, error) {
	var nodes []*Node

	if role == "" {
		nodeList, err := c.GetNodes()
		if err != nil {
			return nil, err
		}
		nodes = nodeList
	} else {
		nodeList, err := c.conn.CoreV1().Nodes().List(
			context.Background(), metav1.ListOptions{LabelSelector: "node-role.kubernetes.io/" + role})
		if err != nil {
			fmt.Fprintf(os.Stderr, err.Error())
			return nil, err
		}

		for _, node := range nodeList.Items {
			nodes = append(nodes, NewNode(&node))
		}
	}

	return nodes, nil
}

func (c *Client) GetPods(namespace string, opts metav1.ListOptions) ([]*Pod, error) {
	var pods []*Pod

	podList, err := c.conn.CoreV1().Pods(namespace).List(
		context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, err
	}

	for _, pod := range podList.Items {
		pods = append(pods, NewPod(&pod))
	}

	return pods, nil
}
