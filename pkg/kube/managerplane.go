package kube

import (
	"context"
	"fmt"

	"github.com/symcn/api"
	"github.com/symcn/pkg/clustermanager/client"
	"github.com/symcn/pkg/clustermanager/configuration"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	ManagerPlaneName          = "clustermanager"
	ManagerPlaneClusterClient api.MingleClient
)

func InitManagerPlaneClusterClient(ctx context.Context) (err error) {
	opts := client.DefaultOptions()
	clusterapiv1.AddToScheme(opts.Scheme)

	ManagerPlaneClusterClient, err = client.NewMingleClient(
		configuration.BuildDefaultClusterCfgInfo(ManagerPlaneName),
		opts,
	)

	if err != nil {
		return fmt.Errorf("init manger-plane cluster client failed: %s", err.Error())
	}

	go ManagerPlaneClusterClient.Start(ctx)

	return nil
}
