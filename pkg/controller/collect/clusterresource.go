package collect

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/symcn/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterStatus struct {
	ClusterName       string
	KubernetesVersion string
	Platform          string
	Healthz           bool
	Livez             bool
	Readyz            bool
	ClusterCIDR       string
	ServiceCIDR       string
	NodeStatistics    NodeStatistics
	Allocatable       corev1.ResourceList
	Capacity          corev1.ResourceList
}

type NodeStatistics struct {
	ReadyNodes    int32
	NotReadyNodes int32
	UnknownNodes  int32
	LostNodes     int32
}

func CollectClusterStatus(cli api.MingleProxyClient) {
	clusterVersion, err := cli.GetKubeInterface().Discovery().ServerVersion()
	if err != nil {
		klog.Warningf("failed to collect kubernetes version: %v", err)
	}

	nodes := &corev1.NodeList{}
	err = cli.GetRuntimeClient().List(context.TODO(), nodes)
	if err != nil {
		klog.Warningf("failed to list nodes: %v", err)
		return
	}
	nodeStatistics := getNodeStatistics(nodes)

	capacity, allocatable := getNodeResource(nodes)

	clusterCIDR, err := discoverClusterCIDR(cli)
	if err != nil {
		klog.Warningf("failed to discover cluster CIDR: %v", err)
	}
	serviceCIDR, err := discoverServiceCIDR(cli)
	if err != nil {
		klog.Warningf("failed to discover service CIDR: %v", err)
	}

	clusterStatus := ClusterStatus{
		ClusterName:       cli.GetClusterCfgInfo().GetName(),
		KubernetesVersion: clusterVersion.GitVersion,
		Platform:          clusterVersion.Platform,
		Healthz:           getHealthStatus(cli, "/healthz"),
		Livez:             getHealthStatus(cli, "/livez"),
		Readyz:            getHealthStatus(cli, "/readyz"),
		ClusterCIDR:       clusterCIDR,
		ServiceCIDR:       serviceCIDR,
		NodeStatistics:    nodeStatistics,
		Allocatable:       allocatable,
		Capacity:          capacity,
	}
	data, _ := json.MarshalIndent(clusterStatus, "", "  ")
	klog.Infof("get cluster status:\n%s", string(data))
}

func getNodeStatistics(nodes *corev1.NodeList) (nodeStatistics NodeStatistics) {
	for _, node := range nodes.Items {
		flag, condition := getNodeCondition(&node.Status, corev1.NodeReady)
		if flag == -1 {
			nodeStatistics.LostNodes++
			continue
		}

		switch condition.Status {
		case corev1.ConditionTrue:
			nodeStatistics.ReadyNodes++
		case corev1.ConditionFalse:
			nodeStatistics.NotReadyNodes++
		case corev1.ConditionUnknown:
			nodeStatistics.UnknownNodes++
		}
	}
	return
}

func getNodeCondition(status *corev1.NodeStatus, conditiontype corev1.NodeConditionType) (int, *corev1.NodeCondition) {
	if status == nil {
		return -1, nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditiontype {
			return i, &status.Conditions[i]
		}
	}
	return -1, nil
}

func getNodeResource(nodes *corev1.NodeList) (Capacity, Allocatable corev1.ResourceList) {
	var capacityCPU, capacityMem, allocatableCPU, allocatableMem resource.Quantity
	for _, node := range nodes.Items {
		capacityCPU.Add(*node.Status.Capacity.Cpu())
		capacityMem.Add(*node.Status.Capacity.Memory())
		allocatableCPU.Add(*node.Status.Allocatable.Cpu())
		allocatableMem.Add(*node.Status.Allocatable.Memory())
	}

	Capacity = make(map[corev1.ResourceName]resource.Quantity)
	Allocatable = make(map[corev1.ResourceName]resource.Quantity)

	Capacity[corev1.ResourceCPU] = capacityCPU
	Capacity[corev1.ResourceMemory] = capacityMem
	Allocatable[corev1.ResourceCPU] = allocatableCPU
	Allocatable[corev1.ResourceMemory] = allocatableMem
	return
}

func discoverClusterCIDR(cli api.MingleProxyClient) (string, error) {
	clusterIPRange := findPodCommandParameter(cli, "kube-apiserver", "--service-cluster-ip-range")
	if clusterIPRange != "" {
		return clusterIPRange, nil
	}
	return "", errors.New("can't get ClusterIPRange")
}

func discoverServiceCIDR(cli api.MingleProxyClient) (string, error) {
	podIPRange := findPodIPRangerKubeController(cli)
	if podIPRange != "" {
		return podIPRange, nil
	}

	podIPRange = findPodIPRangeKubeProxy(cli)
	if podIPRange != "" {
		return podIPRange, nil
	}

	podIPRange = findPodIPRangeFromNodeSpec(cli)
	if podIPRange != "" {
		return podIPRange, nil
	}

	return "", errors.New("can't get PodIPRange")
}

func findPodIPRangerKubeController(cli api.MingleProxyClient) string {
	return findPodCommandParameter(cli, "kube-controller-manager", "--cluster-cidr")
}

func findPodIPRangeKubeProxy(cli api.MingleProxyClient) string {
	return findPodCommandParameter(cli, "kube-proxy", "--cluster-cidr")
}

func findPodIPRangeFromNodeSpec(cli api.MingleProxyClient) string {
	nodes := &corev1.NodeList{}
	err := cli.GetRuntimeClient().List(context.TODO(), nodes)
	if err != nil {
		klog.Errorf("Failed to list nodes: %v", err)
		return ""
	}

	for _, node := range nodes.Items {
		if node.Spec.PodCIDR != "" {
			return node.Spec.PodCIDR
		}
	}
	return ""
}

func findPodCommandParameter(cli api.MingleProxyClient, labelSelectorValue, parameter string) string {
	pod, err := findPod(cli, "component", labelSelectorValue)
	if err != nil || pod == nil {
		return ""
	}
	for _, container := range pod.Spec.Containers {
		if val := getParaValue(container.Command, parameter); val != "" {
			return val
		}
		if val := getParaValue(container.Args, parameter); val != "" {
			return val
		}
	}
	return ""
}

func findPod(cli api.MingleProxyClient, labelSelectorKey, labelSelectorValue string) (*corev1.Pod, error) {
	requirement, err := labels.NewRequirement(labelSelectorKey, selection.Equals, []string{labelSelectorValue})
	if err != nil {
		return nil, err
	}
	labelSelector := labels.NewSelector()
	labelSelector = labelSelector.Add(*requirement)

	pods := &corev1.PodList{}
	// err = cli.GetRuntimeClient().List(context.TODO(), pods, &client.ListOptions{LabelSelector: labels.SelectorFromSet(map[string]string{}{labelSelectorKey:labelSelectorValue})})
	err = cli.GetRuntimeClient().List(context.TODO(), pods, &client.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		klog.Errorf("Failed to list pods by label selector %q: %v", labelSelector, err)
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, nil
	}
	return &pods.Items[0], nil
}

func getParaValue(lists []string, parameter string) string {
	for _, arg := range lists {
		if strings.HasPrefix(arg, parameter) {
			return strings.Split(arg, "=")[1]
		}
		// Handing the case where the command is in the form of /bin/sh -c exec ....
		if strings.Contains(arg, " ") {
			for _, subArg := range strings.Split(arg, " ") {
				if strings.HasPrefix(subArg, parameter) {
					return strings.Split(subArg, "=")[1]
				}
			}
		}
	}
	return ""
}

func getHealthStatus(cli api.MingleProxyClient, path string) bool {
	var statusCode int
	cli.GetKubeInterface().Discovery().RESTClient().Get().AbsPath(path).Do(context.TODO()).StatusCode(&statusCode)
	return statusCode == http.StatusOK
}
