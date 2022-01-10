package accept

import (
	"context"
	"fmt"

	"github.com/champly/clustermanager/pkg/kube"
	"github.com/symcn/api"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
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
}

func New(ctx context.Context) (*Controller, error) {
	ctrl := &Controller{
		ctx:    ctx,
		client: kube.ManagerPlaneClusterClient,
	}

	err := ctrl.client.AddResourceEventHandler(
		&clusterapiv1.ManagedCluster{},
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				managedCluster, ok := obj.(*clusterapiv1.ManagedCluster)
				if !ok {
					return
				}
				if !managedCluster.Spec.HubAcceptsClient {
					ctrl.AutoApprove(types.NamespacedName{
						Namespace: managedCluster.Namespace,
						Name:      managedCluster.Name},
					)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				managedCluster, ok := newObj.(*clusterapiv1.ManagedCluster)
				if !ok {
					return
				}
				if !managedCluster.Spec.HubAcceptsClient {
					ctrl.AutoApprove(types.NamespacedName{
						Namespace: managedCluster.Namespace,
						Name:      managedCluster.Name},
					)
				}
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("AddResourceEventHandler with managedcluster failed:%+v", err)
	}

	return ctrl, nil
}

func (ctrl *Controller) Start() error {
	<-ctrl.ctx.Done()
	return nil
}

func (ctrl *Controller) AutoApprove(key types.NamespacedName) {
	mc := &clusterapiv1.ManagedCluster{}
	err := ctrl.client.Get(key, mc)
	if err != nil {
		klog.Errorf("Get ManagedCluster %s failed:%+v", key.String(), err)
		return
	}

	if mc.Spec.HubAcceptsClient {
		klog.Infof("hubAcceptsClient already set for managed cluster %s", key.String())
		return
	}

	// get csrlist
	csrs := &certificatesv1.CertificateSigningRequestList{}
	err = ctrl.client.List(csrs, &client.ListOptions{
		LabelSelector: labels.Set{clusterLabel: key.Name}.AsSelector(),
	})
	if err != nil {
		klog.Errorf("Get %s CertificateSigningRequestList failed:%+v", key.String(), err)
		return
	}

	if len(csrs.Items) == 0 {
		klog.Warningf("Not found csr with %s, please check registration logic.", key.Name)
		return
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
			klog.Error(err)
			return
		}
	}

	mc.Spec.HubAcceptsClient = true
	err = ctrl.client.Update(mc)
	if err != nil {
		klog.Errorf("Set hubAcceptsClient to true for ManagedCluster %s failed:%+v", key.String(), err)
		return
	}
	klog.Infof("Set hubAcceptsClient to true for ManagedCluster %s", key.String())
	return
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
