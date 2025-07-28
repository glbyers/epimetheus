package talos

import (
	"context"
	"fmt"
	"os"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	conn *client.Client
}

func Connect() *Client {
	var c Client

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := client.New(ctx,
		client.WithDefaultConfig(),
		client.WithGRPCDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		panic(err.Error())
	}

	c.conn = conn

	return &c
}

func (c *Client) GetEtcdStatus(nodes []string) ([]*machineapi.EtcdStatus, error) {
	etcdStatus, err := c.conn.EtcdStatus(client.WithNodes(context.Background(), nodes...))
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, err
	}

	return etcdStatus.Messages, nil
}

func (c *Client) GetEtcdAlarms(nodes []string) ([]*machineapi.EtcdMemberAlarm, error) {
	var alarms []*machineapi.EtcdMemberAlarm

	etcdAlarmListResponse, err := c.conn.EtcdAlarmList(client.WithNodes(context.Background(), nodes...))
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, err
	}

	for _, etcdAlarm := range etcdAlarmListResponse.Messages {
		for _, alarm := range etcdAlarm.MemberAlarms {
			alarms = append(alarms, alarm)
		}
	}

	return alarms, nil
}

func (c *Client) GetServiceList(nodes []string) ([]*machineapi.ServiceList, error) {
	serviceList, err := c.conn.ServiceList(client.WithNodes(context.Background(), nodes...))
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, err
	}

	return serviceList.Messages, nil
}

func (c *Client) GetServiceInfo(nodes []string, service string) ([]client.ServiceInfo, error) {
	services, err := c.conn.ServiceInfo(client.WithNodes(context.Background(), nodes...), service)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		return nil, err
	}

	return services, nil
}
