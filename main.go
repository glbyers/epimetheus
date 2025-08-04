package main

import (
	"github.com/glbyers/epimetheus/k8s"
	"github.com/glbyers/epimetheus/talos"
	"github.com/thanhpk/randstr"
	"os"
	"strings"

	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/alecthomas/units"
	"github.com/gin-gonic/gin"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Server struct {
	k8s   *k8s.Client
	talos *talos.Client
	*gin.Engine
}
type Args struct {
	Listen   string
	Username string
	Password string
	Server   *Server
}

func main() {
	args := setup()

	s := args.Server
	s.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
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
	s.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		if err, ok := recovered.(string); ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		}
		c.AbortWithStatus(http.StatusInternalServerError)
	}))

	// Anonymous endpoint for liveness & readiness probes
	s.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	// All /v1 endpoints require basic auth
	v1 := s.Group("/v1", gin.BasicAuth(gin.Accounts{
		args.Username: args.Password,
	}))

	v1.GET("/service", s.getServiceList)
	v1.GET("/service/:service", s.getService)

	v1.GET("/etcd/alarms", s.getEtcdAlarms)
	v1.GET("/etcd/status", s.getEtcdStatus)

	v1.GET("/pod", s.getPods)
	v1.GET("/pod/:namespace", s.getPods)

	v1.GET("/images", s.getImages)

	{
		nodes := v1.Group("/node")
		nodes.GET("", s.getNodes)
		nodes.GET("/:name", s.getNodeStatus)
		nodes.GET("/:name/service", s.getServiceList)
		nodes.GET("/:name/service/:service", s.getService)
		nodes.GET("/:name/pod", s.getPods)
		nodes.GET("/:name/pod/:namespace", s.getPods)
	}

	s.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"message": "Page not found"})
	})
	s.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{"message": "Method not allowed"})
	})

	if err := s.Run(args.Listen); err != nil {
		panic(err.Error())
	}
}

func setup() *Args {
	args := Args{
		Listen:   getEnv("LISTEN_ADDRESS", "127.0.0.1:8080"),
		Username: getEnv("AUTH_USERNAME", "ghost"),
		Password: os.Getenv("AUTH_PASSWORD"),
	}

	if args.Password == "" {
		args.Password = randstr.String(32)
		fmt.Printf("WARNING: Using randomly generated credentials: %s:%s\n", args.Username, args.Password)
	}

	args.Server = &Server{
		k8s:    k8s.New(),
		talos:  talos.New(),
		Engine: gin.New(),
	}

	if val, ok := os.LookupEnv("TRUSTED_PROXIES"); ok {
		err := args.Server.SetTrustedProxies(strings.Split(val, ","))
		if err != nil {
			contextErr := fmt.Errorf("error setting trusted proxies: %v", err)
			panic(contextErr)
		}
	} else {
		args.Server.SetTrustedProxies(nil)
	}

	return &args
}

func (s *Server) getPods(c *gin.Context) {
	var (
		node      string
		namespace string
		opts      metav1.ListOptions
		response  struct {
			Pods   []*k8s.SimplePod `json:"pods"`
			Errors []string         `json:"errors"`
		}
	)
	node = c.Param("name")
	namespace = c.Param("namespace")
	static := bool(c.Query("static") == "true")

	if node != "" {
		opts.FieldSelector = "spec.nodeName=" + node
	}

	podList, err := s.k8s.GetPods(namespace, opts)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	status := http.StatusOK
	var ok bool

	for _, pod := range podList {
		ok = false
		if static {
			for _, mf := range pod.ManagedFields {
				if mf.Manager == "kubelet" {
					response.Pods = append(response.Pods, &pod.SimplePod)
					ok = true
				}
			}
		} else {
			response.Pods = append(response.Pods, &pod.SimplePod)
			ok = true
		}

		if ok {
			for _, cond := range pod.Pod.Status.Conditions {
				// All conditions except NodeReady should be false
				if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue && cond.Reason != "PodCompleted" {
					status = http.StatusExpectationFailed
					response.Errors = append(response.Errors,
						fmt.Sprintf("Pod '%s/%s' not ready: %s", pod.Namespace, pod.Name, cond.Message))
				}
			}
		}
	}

	c.IndentedJSON(status, response)
}

func (s *Server) getServiceList(c *gin.Context) {
	var (
		name     string
		nodes    []string
		response struct {
			Services []*client.ServiceInfo `json:"services"`
			Errors   []string              `json:"errors"`
		}
	)

	name = c.Param("name")
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
			if nodeList == nil {
				c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
				return
			}
			response.Errors = append(response.Errors, err.Error())
		}

		for _, node := range nodeList {
			nodes = append(nodes, node.Address)
		}
	}

	serviceList, err := s.talos.GetServiceList(nodes)
	if err != nil {
		if serviceList == nil {
			c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		response.Errors = append(response.Errors, err.Error())
	}

	status := http.StatusOK
	// Services that never report healthy, but do report running. Maybe we
	// exclude all extension services?
	healthUnknown := []string{"dashboard", "ext-iscsid", "ext-qemu-guest-agent", "ext-lldpd"}
	for _, nodeService := range serviceList {
		for _, svc := range nodeService.Services {
			response.Services = append(response.Services, &client.ServiceInfo{
				Metadata: nodeService.Metadata,
				Service:  svc,
			})
			// dashboard service never reports healthy, but should report running.
			if !slices.Contains(healthUnknown, svc.Id) && !svc.Health.Healthy {
				status = http.StatusExpectationFailed
				response.Errors = append(
					response.Errors,
					fmt.Sprintf("Service '%s' on %s not healthy", svc.Id, nodeService.Metadata.Hostname))
			} else if svc.State != "Running" {
				status = http.StatusExpectationFailed
				response.Errors = append(
					response.Errors,
					fmt.Sprintf("Service '%s' on %s not running", svc.Id, nodeService.Metadata.Hostname))
			}
		}
	}

	c.IndentedJSON(status, response)
}

func (s *Server) getService(c *gin.Context) {
	var (
		name     string
		service  string
		nodes    []string
		response struct {
			Service []client.ServiceInfo `json:"service"`
			Errors  []string             `json:"errors"`
		}
	)

	name = c.Param("name")
	service = c.Param("service")

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

	services, err := s.talos.GetServiceInfo(nodes, service)
	if err != nil {
		if services == nil {
			c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		response.Errors = append(response.Errors, err.Error())
	}

	status := http.StatusOK
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
	var (
		leader         uint64
		nodes          []string
		etcdStatusList []*machine.EtcdStatus
		minDbSize      units.Base2Bytes
		response       struct {
			Status []*machine.EtcdStatus `json:"status"`
			Errors []string              `json:"errors"`
		}
	)

	minDbSize, _ = units.ParseBase2Bytes(c.DefaultQuery("minDbSize", "512MiB"))

	// fetch internal address for control plane nodes
	nodeList, err := s.k8s.GetNodesByRole("control-plane")
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	for _, node := range nodeList {
		nodes = append(nodes, node.Address)
	}

	etcdStatusList, err = s.talos.GetEtcdStatus(nodes)

	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	status := http.StatusOK
	for _, etcdStatus := range etcdStatusList {
		response.Status = append(response.Status, etcdStatus)
		if leader == 0 {
			leader = etcdStatus.MemberStatus.Leader
		} else if leader != etcdStatus.MemberStatus.Leader {
			status = http.StatusExpectationFailed
			response.Errors = append(response.Errors, "Members don't agree on the same leader")
		}

		// database fragmentation checks
		if etcdStatus.MemberStatus.DbSize > int64(minDbSize) {
			if float64(etcdStatus.MemberStatus.DbSizeInUse/etcdStatus.MemberStatus.DbSize) > 0.5 {
				status = http.StatusExpectationFailed
				response.Errors = append(response.Errors, "db exceeds 50%% fragmentation")
			}
		}
	}
	c.IndentedJSON(status, response)
}

func (s *Server) getEtcdAlarms(c *gin.Context) {
	var nodes []string
	var alarms []*machine.EtcdMemberAlarm

	// fetch internal address for control plane nodes
	nodeList, err := s.k8s.GetNodesByRole("control-plane")
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	for _, node := range nodeList {
		nodes = append(nodes, node.Address)
	}

	alarms, err = s.talos.GetEtcdAlarms(nodes)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	if alarms != nil {
		c.IndentedJSON(http.StatusExpectationFailed, alarms)
		return
	}
	c.IndentedJSON(http.StatusOK, gin.H{"message": "No alarms present"})
}

func (s *Server) getNodes(c *gin.Context) {
	var nodes []*k8s.SimpleNode

	role := c.Query("role")

	nodeList, err := s.k8s.GetNodesByRole(role)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	} else {
		for _, node := range nodeList {
			nodes = append(nodes, &node.SimpleNode)
		}
	}

	c.IndentedJSON(http.StatusOK, nodes)
}

func (s *Server) getNodeStatus(c *gin.Context) {
	var (
		status     int
		nodeStatus k8s.NodeStatus
	)

	node, err := s.k8s.GetNode(c.Param("name"))
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	nodeStatus = node.Status()
	if nodeStatus.Errors != nil {
		status = http.StatusExpectationFailed
	} else {
		status = http.StatusOK
	}
	c.IndentedJSON(status, nodeStatus)
}

func (s *Server) getImages(c *gin.Context) {
	var (
		images []*talos.Image
		nodes  []string
	)

	nodeList, err := s.k8s.GetNodes()
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	for _, node := range nodeList {
		nodes = append(nodes, node.Name)
	}

	images, err = s.talos.GetImageList(nodes, common.ContainerdNamespace_NS_CRI)
	if err != nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.IndentedJSON(http.StatusOK, images)
}
