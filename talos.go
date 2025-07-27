package main

import (
	"context"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func apidConnection() (c *client.Client, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err = client.New(ctx,
		client.WithDefaultConfig(),
		client.WithGRPCDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	return
}
