package accept

import (
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type predicate struct{}

func (p *predicate) Create(obj client.Object) bool {
	return p.handler(obj)
}

func (p *predicate) Update(oldObj, newObj client.Object) bool {
	return p.handler(newObj)
}

func (p *predicate) Delete(obj client.Object) bool {
	return false
}

func (p *predicate) Generic(obj client.Object) bool {
	return false
}

func (p *predicate) handler(obj client.Object) bool {
	managedCluster, ok := obj.(*clusterapiv1.ManagedCluster)
	if !ok {
		return false
	}
	return !managedCluster.Spec.HubAcceptsClient
}
