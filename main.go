package main

import (
	"github.com/glbyers/epimetheus/k8s"

	"context"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/alecthomas/units"

	"github.com/gin-gonic/gin"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ControlPlane string = "node-role.kubernetes.io/control-plane"
	Worker       string = "node-role.kubernetes.io/worker"
)

type Server struct {
	k8s    *k8s.Client
	router *gin.Engine
}

func main() {
	var listen string
	var username string
	var password string
	var proxies []string

	if val, ok := os.LookupEnv("LISTEN_ADDRESS"); ok {
		listen = val
	} else {
		listen = "127.0.0.1:8080"
	}

	if val, ok := os.LookupEnv("AUTH_USERNAME"); ok {
		username = val
	} else {
		username = "zabbix"
	}

	if val, ok := os.LookupEnv("AUTH_PASSWORD"); ok {
		password = val
	} else {
		password = "53kr17"
	}

	if val, ok := os.LookupEnv("TRUSTED_PROXIES"); ok {
		proxies = strings.Split(val, ",")
	}

	r := gin.New()
	s := NewServer(k8s.Connect(), r)

	err := r.SetTrustedProxies(proxies)
	if err != nil {
		panic(err.Error())
	}
	r.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("%s - [%s] \"%s %s %s %d %s \"%s\" %s\"\n",
			param.ClientIP,
			param.TimeStamp.Format(time.RFC1123),
			param.Method,
			param.Path,
			param.Request.Proto,
			param.StatusCode,
			param.Latency,
			param.Request.UserAgent(),
			param.ErrorMessage,
		)
	}))
	r.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		if err, ok := recovered.(string); ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		}
		c.AbortWithStatus(http.StatusInternalServerError)
	}))

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	v1 := r.Group("/v1", gin.BasicAuth(gin.Accounts{
		username: password,
	}))

	v1.GET("/services", s.getServiceList)
	v1.GET("/service/:service", s.getService)

	v1.GET("/etcd/alarms", s.getEtcdAlarms)
	v1.GET("/etcd/status", s.getEtcdStatus)

	{
		nodes := v1.Group("/nodes")
		nodes.GET("", s.getNodes)
		nodes.GET("/:name", s.getNodeStatus)
		nodes.GET("/:name/services", s.getServiceList)
		nodes.GET("/:name/service/:service", s.getService)
		nodes.GET("/:name/pods", s.getPods)
		nodes.GET("/:name/pods/:namespace", s.getPods)
		nodes.GET("/:name/staticpods", s.getStaticPods)
		nodes.GET("/:name/staticpods/:namespace", s.getStaticPods)
	}

	v1.GET("/pods", s.getPods)
	v1.GET("/pods/:namespace", s.getPods)
	v1.GET("/staticpods", s.getStaticPods)
	v1.GET("/staticpods/:namespace", s.getStaticPods)

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"message": "Page not found"})
	})
	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{"message": "Method not allowed"})
	})

	err = s.router.Run(listen)
	if err != nil {
		panic(err.Error())
	}
}

func NewServer(k *k8s.Client, r *gin.Engine) *Server {
	return &Server{
		k8s:    k,
		router: r,
	}
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

func (s *Server) getPods(c *gin.Context) {
	var opts metav1.ListOptions
	var response struct {
		Pods   []*k8s.SimplePod `json:"pods"`
		Errors []string         `json:"errors"`
	}

	node := c.Param("name")
	namespace := c.Param("namespace")

	if node != "" {
		opts.FieldSelector = "spec.nodeName=" + node
	}

	podList, err := s.k8s.GetPods(namespace, opts)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	status := http.StatusOK
	for _, pod := range podList {
		response.Pods = append(response.Pods, &pod.SimplePod)
		for _, cond := range pod.Pod.Status.Conditions {
			// All conditions except NodeReady should be false
			if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue && cond.Reason != "PodCompleted" {
				status = http.StatusExpectationFailed
				response.Errors = append(response.Errors,
					fmt.Sprintf("Pod '%s/%s' not ready: %s", pod.Namespace, pod.Name, cond.Message))
			}
		}
	}

	c.IndentedJSON(status, response)
}

func (s *Server) getStaticPods(c *gin.Context) {
	var opts metav1.ListOptions
	var response struct {
		Pods   []*k8s.SimplePod `json:"pods"`
		Errors []string         `json:"errors"`
	}

	node := c.Param("name")
	namespace := c.Param("namespace")

	if node != "" {
		opts.FieldSelector = "spec.nodeName=" + node
	}

	podList, err := s.k8s.GetPods(namespace, opts)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	status := http.StatusOK
	for _, pod := range podList {
		for _, mf := range pod.ManagedFields {
			if mf.Manager == "kubelet" {
				response.Pods = append(response.Pods, &pod.SimplePod)
			}
		}
		for _, cond := range pod.Pod.Status.Conditions {
			// All conditions except NodeReady should be false
			if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue && cond.Reason != "PodCompleted" {
				status = http.StatusExpectationFailed
				response.Errors = append(response.Errors,
					fmt.Sprintf("Pod '%s/%s' not ready: %s", pod.Namespace, pod.Name, cond.Message))
			}
		}
	}

	c.IndentedJSON(status, response)
}

func (s *Server) getNodeStatus(c *gin.Context) {
	var response struct {
		Status *corev1.NodeStatus `json:"status"`
		Errors []string           `json:"errors"`
	}
	node, err := s.k8s.GetNode(c.Param("name"))
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	status := http.StatusOK
	response.Status = &node.Status
	for _, cond := range node.Status.Conditions {
		// All conditions except NodeReady should be false
		if cond.Type != corev1.NodeReady && cond.Status != corev1.ConditionFalse {
			status = http.StatusExpectationFailed
			response.Errors = append(response.Errors, cond.Message)
		} else if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
			status = http.StatusExpectationFailed
			response.Errors = append(response.Errors, cond.Message)
		}
	}

	c.IndentedJSON(status, response)
}

func (s *Server) getServiceList(c *gin.Context) {
	name := c.Param("name")
	var nodes []string

	apid, err := apidConnection()
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	if name != "" {
		node, err := s.k8s.GetNode(name)
		if err != nil {
			c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		nodes = append(nodes, node.Address)
	} else {
		nodeList, err := s.k8s.GetNodes()
		if err != nil {
			c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		for _, node := range nodeList {
			nodes = append(nodes, node.Address)
		}
	}

	resp, err := apid.ServiceList(client.WithNodes(context.Background(), nodes...))
	if err != nil {
		return
	}

	var response struct {
		Services []client.ServiceInfo `json:"services"`
		Errors   []string             `json:"errors"`
	}
	status := http.StatusOK
	healthUnknown := []string{"dashboard", "ext-iscsid", "ext-qemu-guest-agent"}
	for _, resp := range resp.Messages {
		for _, svc := range resp.Services {
			response.Services = append(response.Services, client.ServiceInfo{
				Metadata: resp.Metadata,
				Service:  svc,
			})
			// dashboard service never reports healthy, but should report running.
			if !slices.Contains(healthUnknown, svc.Id) && !svc.Health.Healthy {
				status = http.StatusExpectationFailed
				response.Errors = append(
					response.Errors,
					fmt.Sprintf("Service '%s' on %s not healthy", svc.Id, resp.Metadata.Hostname))
			} else if svc.State != "Running" {
				status = http.StatusExpectationFailed
				response.Errors = append(
					response.Errors,
					fmt.Sprintf("Service '%s' on %s not running", svc.Id, resp.Metadata.Hostname))
			}
		}
	}
	c.IndentedJSON(status, response)
}

func (s *Server) getService(c *gin.Context) {
	name := c.Param("name")
	service := c.Param("service")
	status := http.StatusOK

	apid, err := apidConnection()
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	var nodes []string
	if name != "" {
		node, err := s.k8s.GetNode(name)
		if err != nil {
			c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		nodes = append(nodes, node.Address)
	} else {
		nodeList, err := s.k8s.GetNodes()
		if err != nil {
			c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		for _, node := range nodeList {
			nodes = append(nodes, node.Address)
		}
	}

	services, err := apid.ServiceInfo(client.WithNodes(context.Background(), nodes...), service)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	var response struct {
		Service []client.ServiceInfo `json:"service"`
		Errors  []string             `json:"errors"`
	}

	response.Service = services
	healthUnknown := []string{"dashboard", "ext-iscsid", "ext-qemu-guest-agent"}
	for _, svc := range services {
		// dashboard service never reports healthy, but should report running.
		if !slices.Contains(healthUnknown, svc.Service.Id) && !svc.Service.Health.Healthy {
			status = http.StatusExpectationFailed
			response.Errors = append(
				response.Errors,
				fmt.Sprintf("Service '%s' on %s not healthy", svc.Service.Id, svc.Metadata.Hostname))
		} else if svc.Service.State != "Running" {
			status = http.StatusExpectationFailed
			response.Errors = append(
				response.Errors,
				fmt.Sprintf("Service '%s' on %s not running", svc.Service.Id, svc.Metadata.Hostname))
		}
	}

	c.IndentedJSON(status, response)
}

func (s *Server) getEtcdStatus(c *gin.Context) {
	var response struct {
		Status []*machineapi.EtcdStatus `json:"status"`
		Errors []string                 `json:"errors"`
	}

	var nodes []string
	minDbSize, _ := units.ParseBase2Bytes(c.DefaultQuery("minDbSize", "512MiB"))

	apid, err := apidConnection()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// fetch internal address for control plane nodes
	nodeList, err := s.k8s.GetNodesByLabel(ControlPlane)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	for _, node := range nodeList {
		nodes = append(nodes, node.Address)
	}

	resp, err := apid.EtcdStatus(client.WithNodes(context.Background(), nodes...))

	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	var leader uint64
	status := http.StatusOK
	for _, resp := range resp.Messages {
		//status := resp.MemberStatus
		response.Status = append(response.Status, resp)
		if leader == 0 {
			leader = resp.MemberStatus.Leader
		} else if leader != resp.MemberStatus.Leader {
			status = http.StatusExpectationFailed
			response.Errors = append(response.Errors, "Members don't agree on the same leader")
		}

		// database fragmentation checks
		if resp.MemberStatus.DbSize > int64(minDbSize) {
			if float64(resp.MemberStatus.DbSizeInUse/resp.MemberStatus.DbSize) > 0.5 {
				status = http.StatusExpectationFailed
				response.Errors = append(response.Errors, "db exceeds 50%% fragmentation")
			}
		}
	}
	c.IndentedJSON(status, response)
}

func (s *Server) getEtcdAlarms(c *gin.Context) {
	var nodes []string
	var alarms []*machineapi.EtcdMemberAlarm

	apid, err := apidConnection()
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	// fetch internal address for control plane nodes
	nodeList, err := s.k8s.GetNodesByLabel(ControlPlane)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	for _, node := range nodeList {
		nodes = append(nodes, node.Address)
	}

	resp, err := apid.EtcdAlarmList(client.WithNodes(context.Background(), nodes...))

	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	for _, resp := range resp.Messages {
		for _, alarm := range resp.MemberAlarms {
			alarms = append(alarms, alarm)
		}
	}

	if alarms != nil {
		c.IndentedJSON(http.StatusExpectationFailed, alarms)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "No alarms present"})
}

func (s *Server) getNodes(c *gin.Context) {
	var nodes []*k8s.SimpleNode
	var label string

	role := c.Query("role")
	if role != "" {
		label = "node-role.kubernetes.io/" + role
	}

	nodeList, err := s.k8s.GetNodesByLabel(label)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	} else {
		for _, node := range nodeList {
			nodes = append(nodes, &node.SimpleNode)
		}
	}

	c.IndentedJSON(http.StatusOK, nodes)
}
