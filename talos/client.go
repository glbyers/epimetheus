package talos

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/siderolabs/talos/cmd/talosctl/pkg/talos/helpers"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

type Image struct {
	Node      string
	Name      string
	Digest    string
	Size      string
	CreatedAt string
}
type Client struct {
	*client.Client
}

func New() *Client {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// default config in k8s will be whatever is mounted at /var/run/secrets/talos.dev/config
	// default endpoint will be port 50000 on the local node
	c, err := client.New(ctx, client.WithDefaultConfig())
	if err != nil {
		panic(err.Error())
	}

	return &Client{c}
}

func (c *Client) GetEtcdStatus(nodes []string) ([]*machine.EtcdStatus, error) {
	etcdStatus, err := c.EtcdStatus(client.WithNodes(context.Background(), nodes...))
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}
	if etcdStatus == nil {
		return nil, fmt.Errorf("error getting status: %w", err)
	}

	return etcdStatus.Messages, err
}

func (c *Client) GetEtcdAlarms(nodes []string) ([]*machine.EtcdMemberAlarm, error) {
	var alarms []*machine.EtcdMemberAlarm

	etcdAlarmListResponse, err := c.EtcdAlarmList(client.WithNodes(context.Background(), nodes...))
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}
	if etcdAlarmListResponse == nil {
		return nil, fmt.Errorf("error getting status: %w", err)
	}

	for _, etcdAlarm := range etcdAlarmListResponse.Messages {
		for _, alarm := range etcdAlarm.MemberAlarms {
			alarms = append(alarms, alarm)
		}
	}

	return alarms, err
}

func (c *Client) GetServiceList(nodes []string) ([]*machine.ServiceList, error) {
	serviceList, err := c.ServiceList(client.WithNodes(context.Background(), nodes...))
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}
	if serviceList == nil {
		return nil, fmt.Errorf("error listing services: %w", err)
	}

	return serviceList.Messages, err
}

func (c *Client) GetServiceInfo(nodes []string, service string) ([]client.ServiceInfo, error) {
	services, err := c.ServiceInfo(client.WithNodes(context.Background(), nodes...), service)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}
	if services == nil {
		return nil, fmt.Errorf("error getting service info: %w", err)
	}

	return services, err
}

func (c *Client) GetImageList(nodes []string, namespace common.ContainerdNamespace) ([]*Image, error) {
	var images []*Image

	rcv, err := c.ImageList(client.WithNodes(context.Background(), nodes...), namespace)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, fmt.Errorf("error listing images: %w", err)
	}

	if err = helpers.ReadGRPCStream(rcv, func(msg *machine.ImageListResponse, node string, multipleNodes bool) error {
		images = append(images, &Image{
			Node:      node,
			Name:      msg.Name,
			Digest:    msg.Digest,
			Size:      humanize.Bytes(uint64(msg.Size)),
			CreatedAt: msg.CreatedAt.AsTime().Format(time.RFC3339),
		})
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, err
	}

	return images, nil
}
