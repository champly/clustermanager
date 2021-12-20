package collect

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/symcn/api"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

var (
	ShowLabelKey = "app"
)

type SummaryResourceUseage struct {
	DeploymentStatistics  DeploymentStatistics
	StatefulsetStatistics StatefulsetStatistics
	DaemonsetStatistics   DaemonsetStatistics
}

type DeploymentStatistics struct {
	List map[string][]DeploymentStatus
}

type StatefulsetStatistics struct {
	List map[string][]StatefulsetStatus
}

type DaemonsetStatistics struct {
	List map[string][]DaemonSetStatus
}

type DeploymentStatus struct {
	Name                string
	ShowName            string
	Replicas            int32
	ReadyReplicas       int32
	UnavailableReplicas int32
	Resource            Resouces
}

type StatefulsetStatus struct {
	Name          string
	ShowName      string
	Replicas      int32
	ReadyReplicas int32
	Resource      Resouces
}

type DaemonSetStatus struct {
	Name              string
	ShowName          string
	NumberAvailable   int32
	NumberUnavailable int32
	CollisionCount    *int32
	Resource          Resouces
}

type Resouces struct {
	Requests corev1.ResourceList
	Limits   corev1.ResourceList
}

func CollectDeploymentStatus(cli api.MingleProxyClient) {
	// deployment
	deploymentStatistics := DeploymentStatistics{List: map[string][]DeploymentStatus{}}
	deploys, err := getAllDeployment(cli)
	if err != nil {
		klog.Warning(err)
	} else {

		for _, deploy := range deploys.Items {
			if _, ok := deploymentStatistics.List[deploy.Namespace]; !ok {
				deploymentStatistics.List[deploy.Namespace] = []DeploymentStatus{}
			}
			deploymentStatistics.List[deploy.Namespace] = append(deploymentStatistics.List[deploy.Namespace], buildDeploymentStatus(&deploy))
		}
	}

	// statefulset
	statefulsetStatistics := StatefulsetStatistics{List: map[string][]StatefulsetStatus{}}
	statefulsets, err := getAllStatefulset(cli)
	if err != nil {
		klog.Warning(err)
	} else {

		for _, statefulset := range statefulsets.Items {
			if _, ok := statefulsetStatistics.List[statefulset.Namespace]; !ok {
				statefulsetStatistics.List[statefulset.Namespace] = []StatefulsetStatus{}
			}
			statefulsetStatistics.List[statefulset.Namespace] = append(statefulsetStatistics.List[statefulset.Namespace], buildStatefulsetStatus(&statefulset))
		}
	}

	// daemonset
	daemonsetStatistics := DaemonsetStatistics{List: map[string][]DaemonSetStatus{}}
	daemonsets, err := getAllDeamonset(cli)
	if err != nil {
		klog.Warning(err)
	} else {

		for _, daemonset := range daemonsets.Items {
			if _, ok := daemonsetStatistics.List[daemonset.Namespace]; !ok {
				daemonsetStatistics.List[daemonset.Namespace] = []DaemonSetStatus{}
			}
			daemonsetStatistics.List[daemonset.Namespace] = append(daemonsetStatistics.List[daemonset.Namespace], buildDaemonsetStatus(&daemonset))
		}
	}

	summary := SummaryResourceUseage{
		DeploymentStatistics:  deploymentStatistics,
		StatefulsetStatistics: statefulsetStatistics,
		DaemonsetStatistics:   daemonsetStatistics,
	}

	// data, _ := json.MarshalIndent(summary, "", "  ")
	data, _ := json.Marshal(summary)
	klog.Infof("get summary resource useage status:\n%s", string(data))
}

func getAllDeployment(cli api.MingleProxyClient) (*appsv1.DeploymentList, error) {
	deploys := &appsv1.DeploymentList{}
	err := cli.GetRuntimeClient().List(context.TODO(), deploys)
	if err != nil {
		return nil, fmt.Errorf("get all deployments failed: %+v", err)
	}
	return deploys, nil
}
func buildDeploymentStatus(deploy *appsv1.Deployment) DeploymentStatus {
	ds := DeploymentStatus{
		Name:                deploy.Name,
		ShowName:            deploy.Labels[ShowLabelKey],
		Replicas:            deploy.Status.Replicas,
		ReadyReplicas:       deploy.Status.ReadyReplicas,
		UnavailableReplicas: deploy.Status.UnavailableReplicas,
		Resource:            buildResource(deploy.Spec.Template.Spec.Containers),
	}
	return ds
}

func getAllStatefulset(cli api.MingleProxyClient) (*appsv1.StatefulSetList, error) {
	statefulsets := &appsv1.StatefulSetList{}
	err := cli.GetRuntimeClient().List(context.TODO(), statefulsets)
	if err != nil {
		return nil, fmt.Errorf("get all statefulset failed: %+v", err)
	}
	return statefulsets, nil
}

func buildStatefulsetStatus(statefulset *appsv1.StatefulSet) StatefulsetStatus {
	ss := StatefulsetStatus{
		Name:          statefulset.Name,
		ShowName:      statefulset.Labels[ShowLabelKey],
		Replicas:      statefulset.Status.Replicas,
		ReadyReplicas: statefulset.Status.AvailableReplicas,
		Resource:      buildResource(statefulset.Spec.Template.Spec.Containers),
	}
	return ss
}

func getAllDeamonset(cli api.MingleProxyClient) (*appsv1.DaemonSetList, error) {
	daemonsets := &appsv1.DaemonSetList{}
	err := cli.GetRuntimeClient().List(context.TODO(), daemonsets)
	if err != nil {
		return nil, fmt.Errorf("get all daemonset failed: %+v", err)
	}
	return daemonsets, nil
}

func buildDaemonsetStatus(daemonset *appsv1.DaemonSet) DaemonSetStatus {
	ds := DaemonSetStatus{
		Name:              daemonset.Name,
		ShowName:          daemonset.Labels[ShowLabelKey],
		NumberAvailable:   daemonset.Status.NumberAvailable,
		NumberUnavailable: daemonset.Status.NumberUnavailable,
		CollisionCount:    daemonset.Status.CollisionCount,
		Resource:          buildResource(daemonset.Spec.Template.Spec.Containers),
	}
	return ds
}
func buildResource(list []corev1.Container) Resouces {
	rs := Resouces{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	for _, container := range list {
		for rname, value := range container.Resources.Requests {
			r, ok := rs.Requests[corev1.ResourceName(rname)]
			if !ok {
				r = resource.Quantity{}
			}
			r.Add(value)
			rs.Requests[corev1.ResourceName(rname)] = r
		}

		for rname, value := range container.Resources.Limits {
			r, ok := rs.Limits[corev1.ResourceName(rname)]
			if !ok {
				r = resource.Quantity{}
			}
			r.Add(value)
			rs.Limits[corev1.ResourceName(rname)] = r
		}
	}
	return rs
}
