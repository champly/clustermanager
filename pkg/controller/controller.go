package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/champly/clustermanager/pkg/controller/collect"
	"github.com/champly/clustermanager/pkg/kube"
	clustetgatewayv1aplpha1 "github.com/oam-dev/cluster-gateway/pkg/apis/cluster/v1alpha1"
	"github.com/symcn/api"
	symcnClient "github.com/symcn/pkg/clustermanager/client"
	"github.com/symcn/pkg/clustermanager/configuration"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

var CollectInterval = time.Second * 20

var (
	scheme = runtime.NewScheme()
)

type Controller struct {
	ctx context.Context
	api.MultiProxyClient
	gvr schema.GroupVersionResource
}

func New(ctx context.Context) (*Controller, error) {
	clustetgatewayv1aplpha1.AddToScheme(scheme)
	clientgoscheme.AddToScheme(scheme)
	workapiv1.AddToScheme(scheme)

	dynamicClient := dynamic.NewForConfigOrDie(kube.ManagerPlaneClusterClient.GetKubeRestConfig())
	ccm := configuration.NewClusterCfgManagerWithGateway(dynamicClient, kube.ManagerPlaneClusterClient.GetClusterCfgInfo())

	mpc := symcnClient.NewMingleProxyClient(ccm, scheme)

	return &Controller{
		ctx:              ctx,
		MultiProxyClient: mpc,
		gvr: schema.GroupVersionResource{
			Group:    workapiv1.GroupVersion.Group,
			Version:  workapiv1.GroupVersion.Version,
			Resource: "appliedmanifestworks",
		},
	}, nil
}

func (ctrl *Controller) Start() error {
	var err error
	t := time.NewTimer(CollectInterval)
END:
	for {
		select {
		case <-ctrl.ctx.Done():
			break END
		case <-t.C:
			err = ctrl.collect()
			if err != nil {
				klog.Error(err)
			}
			fmt.Println("------------")
			fmt.Println("------------")

			t.Reset(CollectInterval)
		}
	}
	return nil
}

func (ctrl *Controller) collect() error {
	for _, cli := range ctrl.GetAll() {
		collect.CollectClusterStatus(cli)
		collect.CollectDeploymentStatus(cli)
	}
	return nil
}
