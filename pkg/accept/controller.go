package accept

import (
	"context"
	"fmt"
	"time"

	"github.com/champly/clustermanager/pkg/kube"
	"github.com/symcn/api"
	"github.com/symcn/pkg/clustermanager/handler"
	"github.com/symcn/pkg/clustermanager/workqueue"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	clusterLabel   = "open-cluster-management.io/cluster-name"
	controllerName = "ClusterManagerAutoAccept"
)

type Controller struct {
	ctx    context.Context
	client api.MingleClient
	queue  api.WorkQueue
}

func New(ctx context.Context) (*Controller, error) {
	ctrl := &Controller{
		ctx:    ctx,
		client: kube.ManagerPlaneClusterClient,
	}

	queue, err := workqueue.Complted(workqueue.NewQueueConfig(ctrl)).NewQueue()
	if err != nil {
		return nil, fmt.Errorf("Build workqueue failed:%+v", err)
	}
	ctrl.queue = queue

	err = ctrl.client.AddResourceEventHandler(
		&clusterapiv1.ManagedCluster{},
		handler.NewResourceEventHandler(
			ctrl.queue,
			handler.NewDefaultTransformNamespacedNameEventHandler(),
			&predicate{},
		),
	)
	if err != nil {
		return nil, fmt.Errorf("AddResourceEventHandler with managedcluster failed:%+v", err)
	}

	return ctrl, nil
}

func (ctrl *Controller) Start() error {
	return ctrl.queue.Start(ctrl.ctx)
}

func (ctrl *Controller) Reconcile(key types.NamespacedName) (api.NeedRequeue, time.Duration, error) {
	mc := &clusterapiv1.ManagedCluster{}
	err := ctrl.client.Get(key, mc)
	if err != nil {
		return api.Done, 0, fmt.Errorf("Get ManagedCluster %s failed:%+v", key.String(), err)
	}

	if mc.Spec.HubAcceptsClient {
		klog.Infof("hubAcceptsClient already set for managed cluster %s", key.String())
		return api.Done, 0, nil
	}

	// get csrlist
	csrs := &certificatesv1.CertificateSigningRequestList{}
	err = ctrl.client.List(csrs, &client.ListOptions{
		LabelSelector: labels.Set{clusterLabel: key.Name}.AsSelector(),
	})
	if err != nil {
		return api.Done, 0, fmt.Errorf("Get %s CertificateSigningRequestList failed:%+v", key.String(), err)
	}

	if len(csrs.Items) == 0 {
		klog.Warningf("Not found csr with %s, please check registration logic.", key.Name)
		return api.Requeue, time.Second * 5, nil
	}
	for _, item := range csrs.Items {
		approved, denied := getCertApprovalCondition(&item.Status)
		if approved {
			klog.Warningf("CSR %s already approved.", item.Name)
			continue
		}
		if denied {
			klog.Warningf("CSR %s already denied.", item.Name)
			continue
		}
		if err = ctrl.approveCSR(&item); err != nil {
			return api.Done, 0, err
		}
	}

	mc.Spec.HubAcceptsClient = true
	err = ctrl.client.Update(mc)
	if err != nil {
		return api.Done, 0, fmt.Errorf("Set hubAcceptsClient to true for ManagedCluster %s failed:%+v", key.String(), err)
	}
	klog.Infof("Set hubAcceptsClient to true for ManagedCluster %s", key.String())
	return api.Done, 0, nil
}

func (ctrl *Controller) approveCSR(csr *certificatesv1.CertificateSigningRequest) error {
	if csr.Status.Conditions == nil {
		csr.Status.Conditions = make([]certificatesv1.CertificateSigningRequestCondition, 0)
	}
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Status:         corev1.ConditionTrue,
		Type:           certificatesv1.CertificateApproved,
		Reason:         fmt.Sprintf("%sApprove", controllerName),
		Message:        fmt.Sprintf("This CSR was approved by %s certificate approve.", controllerName),
		LastUpdateTime: metav1.Now(),
	})
	signingRequest := ctrl.client.GetKubeInterface().CertificatesV1().CertificateSigningRequests()
	_, err := signingRequest.UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("CSR %s approve failed:%+v", csr.Name, err)
	}
	klog.Infof("CSR %s approved by %s.", csr.Name, controllerName)
	return nil
}

func getCertApprovalCondition(status *certificatesv1.CertificateSigningRequestStatus) (approved, denied bool) {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			approved = true
		}
		if c.Type == certificatesv1.CertificateDenied {
			denied = true
		}
	}
	return
}
