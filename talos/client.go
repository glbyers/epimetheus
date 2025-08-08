package talos

import (
	"context"
	"fmt"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/siderolabs/go-retry/retry"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"os"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/siderolabs/talos/cmd/talosctl/pkg/talos/helpers"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	timeapi "github.com/siderolabs/talos/pkg/machinery/api/time"
)

type Image struct {
	Node      string
	Name      string
	Digest    string
	Size      string
	CreatedAt string
}
type Client struct {
	apid *client.Client
}

func New(ctx context.Context) (*Client, error) {
	// default config in k8s will be whatever is mounted at /var/run/secrets/talos.dev/config
	// default endpoint will be port 50000 on the local node
	c, err := client.New(ctx, client.WithDefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to reinitialized talos client: %v", err)
	}

	return &Client{c}, err
}

func (c *Client) GetEtcdStatus(ctx context.Context, nodes []string) ([]*machine.EtcdStatus, error) {
	var etcdStatus *machine.EtcdStatusResponse

	nodesCtx := client.WithNodes(ctx, nodes...)

	err := retry.Constant(10*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(func() error {
		var getErr error

		etcdStatus, getErr = c.apid.EtcdStatus(nodesCtx)
		if getErr != nil {
			err := c.refreshConnection(ctx)
			if err != nil {
				return retry.ExpectedError(err)
			}

			return getErr
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error getting etcd status: %w", err)
	}
	if etcdStatus == nil {
		return nil, fmt.Errorf("error getting status: %w", err)
	}

	return etcdStatus.Messages, err
}

func (c *Client) GetEtcdAlarms(ctx context.Context, nodes []string) ([]*machine.EtcdMemberAlarm, error) {
	var alarms []*machine.EtcdMemberAlarm
	var etcdAlarmListResponse *machine.EtcdAlarmListResponse

	nodesCtx := client.WithNodes(ctx, nodes...)

	err := retry.Constant(10*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(func() error {
		var getErr error

		etcdAlarmListResponse, getErr = c.apid.EtcdAlarmList(nodesCtx)
		if getErr != nil {
			err := c.refreshConnection(ctx)
			if err != nil {
				return retry.ExpectedError(err)
			}

			return getErr
		}

		return nil
	})

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

func (c *Client) GetServiceList(ctx context.Context, nodes []string) ([]*machine.ServiceList, error) {
	var serviceList *machine.ServiceListResponse

	nodesCtx := client.WithNodes(ctx, nodes...)

	err := retry.Constant(10*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(func() error {
		var getErr error

		serviceList, getErr = c.apid.ServiceList(nodesCtx)
		if getErr != nil {
			err := c.refreshConnection(ctx)
			if err != nil {
				return retry.ExpectedError(err)
			}

			return getErr
		}

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}
	if serviceList == nil {
		return nil, fmt.Errorf("error listing services: %w", err)
	}

	return serviceList.Messages, err
}

func (c *Client) GetServiceInfo(ctx context.Context, nodes []string, service string) ([]client.ServiceInfo, error) {
	var services []client.ServiceInfo

	nodesCtx := client.WithNodes(ctx, nodes...)

	err := retry.Constant(10*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(func() error {
		var getErr error

		services, getErr = c.apid.ServiceInfo(nodesCtx, service)
		if getErr != nil {
			err := c.refreshConnection(ctx)
			if err != nil {
				return retry.ExpectedError(err)
			}

			return getErr
		}

		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}
	if services == nil {
		return nil, fmt.Errorf("error getting service info: %w", err)
	}

	return services, err
}

func (c *Client) GetImageList(ctx context.Context, nodes []string, namespace common.ContainerdNamespace) ([]*Image, error) {
	var images []*Image
	var rcv machine.MachineService_ImageListClient

	nodesCtx := client.WithNodes(ctx, nodes...)

	err := retry.Constant(10*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(func() error {
		var getErr error

		rcv, getErr = c.apid.ImageList(nodesCtx, namespace)
		if getErr != nil {
			err := c.refreshConnection(ctx)
			if err != nil {
				return retry.ExpectedError(err)
			}

			return getErr
		}

		return nil
	})
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

func (c *Client) GetNodeMetadata(ctx context.Context, node string) (*runtime.PlatformMetadataSpec, error) {
	nodeCtx := client.WithNode(ctx, node)

	var resources resource.Resource

	err := retry.Constant(10*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(func() error {
		var getErr error

		resources, getErr = c.apid.COSI.Get(nodeCtx, resource.NewMetadata(runtime.NamespaceName,
			runtime.PlatformMetadataType, runtime.PlatformMetadataID, resource.VersionUndefined))
		if getErr != nil {
			err := c.refreshConnection(ctx)
			if err != nil {
				return retry.ExpectedError(err)
			}

			return getErr
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error get resources: %w", err)
	}

	meta := resources.Spec().(*runtime.PlatformMetadataSpec).DeepCopy()

	return &meta, nil
}

func (c *Client) GetNodeSystemInfo(ctx context.Context, node string) (*hardware.SystemInformationSpec, error) {
	var resources resource.Resource

	nodeCtx := client.WithNode(ctx, node)

	err := retry.Constant(10*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(func() error {
		var getErr error

		resources, getErr = c.apid.COSI.Get(nodeCtx, resource.NewMetadata(
			hardware.NamespaceName, hardware.SystemInformationType, hardware.SystemInformationID,
			resource.VersionUndefined))
		if getErr != nil {
			err := c.refreshConnection(ctx)
			if err != nil {
				return retry.ExpectedError(err)
			}

			return getErr
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error get resources: %w", err)
	}

	meta := resources.Spec().(*hardware.SystemInformationSpec).DeepCopy()

	return &meta, nil
}

func (c *Client) refreshConnection(ctx context.Context) error {
	if _, err := c.apid.Version(ctx); err != nil {
		talos, err := New(ctx)
		if err != nil {
			return fmt.Errorf("failed to reinitialized talos client: %v", err)
		}

		c.apid.Close()
		c.apid = talos.apid
	}

	return nil
}

func (c *Client) TimeCheck(ctx context.Context, ntpServer string) (*timeapi.TimeResponse, error) {
	return c.apid.TimeCheck(context.Background(), ntpServer)
}
