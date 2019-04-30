package watcher

import (
	"fmt"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

const (
	kubeSystem = "kube-system"
	podIPIndex = "ip"
)

// TODO: prom metrics for all the queues/caches
// https://github.com/linkerd/linkerd2/issues/2204

type (
	Address struct {
		Ip        string
		Port      Port
		Pod       *corev1.Pod
		OwnerName string
		OwnerKind string
	}

	PodSet = map[PodID]Address

	// endpointsWatcher watches all endpoints and services in the Kubernetes
	// cluster.  Listeners can subscribe to a particular service and port and
	// endpointsWatcher will publish the address set and all future changes for
	// that service:port.
	EndpointsWatcher struct {
		publishers   map[ServiceID]*servicePublisher
		publishersMu sync.RWMutex
		k8sAPI       *k8s.API

		log *logging.Entry
	}

	// servicePublisher represents a service along with a port number.  Multiple
	// listeners may be subscribed to a servicePublisher.  servicePublisher maintains the
	// current state of the address set and publishes diffs to all listeners when
	// updates come from either the endpoints API or the service API.
	servicePublisher struct {
		id     ServiceID
		log    *logging.Entry
		k8sAPI *k8s.API

		ports   map[Port]*portPublisher
		portsMu sync.RWMutex
	}

	portPublisher struct {
		id         ServiceID
		targetPort namedPort
		log        *logging.Entry
		k8sAPI     *k8s.API

		exists    bool
		pods      PodSet
		listeners []EndpointUpdateListener
	}

	EndpointUpdateListener interface {
		Add(set PodSet)
		Remove(set PodSet)
		NoEndpoints(exists bool)
	}
)

func NewEndpointsWatcher(k8sAPI *k8s.API, log *logging.Entry) *EndpointsWatcher {
	ew := &EndpointsWatcher{
		publishers: make(map[ServiceID]*servicePublisher),
		k8sAPI:     k8sAPI,
		log: log.WithFields(logging.Fields{
			"component": "endpoints-watcher",
		}),
	}

	k8sAPI.Pod().Informer().AddIndexers(cache.Indexers{podIPIndex: func(obj interface{}) ([]string, error) {
		if pod, ok := obj.(*corev1.Pod); ok {
			return []string{pod.Status.PodIP}, nil
		}
		return []string{""}, fmt.Errorf("object is not a pod")
	}})

	k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ew.addService,
		DeleteFunc: ew.deleteService,
		UpdateFunc: func(_, obj interface{}) { ew.addService(obj) },
	})

	k8sAPI.Endpoint().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ew.addEndpoints,
		DeleteFunc: ew.deleteEndpoints,
		UpdateFunc: func(_, obj interface{}) { ew.addEndpoints(obj) },
	})

	return ew
}

////////////////////////
/// EndpointsWatcher ///
////////////////////////

// Subscribe to a service and service port.
// The provided listener will be updated each time the address set for the
// given service port is changed.
func (e *EndpointsWatcher) Subscribe(service ServiceID, port Port, listener EndpointUpdateListener) {
	e.log.Infof("Establishing watch on endpoint %s:%d", service, port)

	e.publishersMu.Lock()
	// If the service doesn't yet exist, create a stub for it so the listener can
	// be registered.
	sp, ok := e.publishers[service]
	if !ok {
		e.publishers[service] = e.newServicePublisher(service)
	}
	e.publishersMu.Unlock()

	sp.subscribe(port, listener)
}

func (e *EndpointsWatcher) Unsubscribe(service ServiceID, port Port, listener EndpointUpdateListener) {
	e.log.Infof("Stopping watch on endpoint %s:%d", service, port)

	e.publishersMu.Lock()
	sp, ok := e.publishers[service]
	if !ok {
		return
	}
	e.publishersMu.Unlock()
	sp.unsubscribe(port, listener)

	return
}

func (e *EndpointsWatcher) addService(obj interface{}) {
	service := obj.(*corev1.Service)
	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	e.publishersMu.Lock()
	sp, ok := e.publishers[id]
	if !ok {
		sp = e.newServicePublisher(id)
		e.publishers[id] = sp
	}
	e.publishersMu.Unlock()

	sp.updateService(service)
}

func (e *EndpointsWatcher) deleteService(obj interface{}) {
	service := obj.(*corev1.Service)
	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	e.publishersMu.Lock()
	defer e.publishersMu.Unlock()

	sp, ok := e.publishers[id]
	if ok {
		sp.deleteEndpoints()
	}
}

func (e *EndpointsWatcher) addEndpoints(obj interface{}) {
	endpoints := obj.(*corev1.Endpoints)
	if endpoints.Namespace == kubeSystem {
		return
	}
	id := ServiceID{
		Namespace: endpoints.Namespace,
		Name:      endpoints.Name,
	}

	e.publishersMu.RLock()
	sp, ok := e.publishers[id]
	if !ok {
		sp = e.newServicePublisher(id)
		e.publishers[id] = sp

	}
	e.publishersMu.RUnlock()

	sp.updateEndpoints(endpoints)
}

func (e *EndpointsWatcher) deleteEndpoints(obj interface{}) {
	endpoints := obj.(*corev1.Endpoints)
	if endpoints.Namespace == kubeSystem {
		return
	}
	id := ServiceID{
		Namespace: endpoints.Namespace,
		Name:      endpoints.Name,
	}

	e.publishersMu.RLock()
	sp, ok := e.publishers[id]
	e.publishersMu.RUnlock()
	if ok {
		sp.deleteEndpoints()
	}
}

func (e *EndpointsWatcher) newServicePublisher(service ServiceID) *servicePublisher {
	return &servicePublisher{
		id: service,
		log: e.log.WithFields(logging.Fields{
			"component": "service-publisher",
			"ns":        service.Namespace,
			"svc":       service.Name,
		}),
		k8sAPI: e.k8sAPI,
		ports:  make(map[Port]*portPublisher),
	}
}

////////////////////////
/// servicePublisher ///
////////////////////////

func (sp *servicePublisher) updateEndpoints(newEndpoints *corev1.Endpoints) {
	sp.portsMu.Lock()
	defer sp.portsMu.Unlock()

	for _, port := range sp.ports {
		port.updateEndpoints(newEndpoints)
	}
}

func (sp *servicePublisher) deleteEndpoints() {
	sp.log.Debugf("Deleting endpoints for %s", sp.id)

	sp.portsMu.Lock()
	defer sp.portsMu.Unlock()

	for _, port := range sp.ports {
		port.noEndpoints(false)
	}
}

func (sp *servicePublisher) updateService(newService *corev1.Service) {
	sp.portsMu.Lock()
	defer sp.portsMu.Unlock()

	for srcPort, port := range sp.ports {
		newTargetPort := getTargetPort(newService, srcPort)
		if newTargetPort != port.targetPort {
			port.updatePort(newTargetPort)
		}
	}
}

func (sp *servicePublisher) subscribe(srcPort Port, listener EndpointUpdateListener) {
	sp.log.Debugf("Subscribing %s:%d", sp.id, srcPort)

	sp.portsMu.Lock()
	defer sp.portsMu.Unlock()

	port, ok := sp.ports[srcPort]
	if !ok {
		sp.ports[srcPort] = sp.newPortPublisher(srcPort)
	}
	port.subscribe(listener)
}

// unsubscribe returns true iff the listener was found and removed.
// it also returns the number of listeners remaining after unsubscribing.
func (sp *servicePublisher) unsubscribe(srcPort Port, listener EndpointUpdateListener) {
	sp.log.Debugf("Unsubscribing %s:%d", sp.id, srcPort)

	sp.portsMu.Lock()
	defer sp.portsMu.Unlock()

	port, ok := sp.ports[srcPort]
	if ok {
		port.unsubscribe(listener)
	}
}

func (sp *servicePublisher) newPortPublisher(srcPort Port) *portPublisher {
	targetPort := intstr.FromInt(int(srcPort))
	svc, err := sp.k8sAPI.Svc().Lister().Services(sp.id.Namespace).Get(sp.id.Name)
	if err != nil {
		targetPort = getTargetPort(svc, srcPort)
	}

	port := &portPublisher{
		listeners:  []EndpointUpdateListener{},
		targetPort: targetPort,
		k8sAPI:     sp.k8sAPI,
		log: sp.log.WithFields(logging.Fields{
			"port": srcPort,
		}),
	}

	endpoints, err := sp.k8sAPI.Endpoint().Lister().Endpoints(sp.id.Namespace).Get(sp.id.Name)
	if err == nil {
		port.updateEndpoints(endpoints)
	} else {
		port.noEndpoints(false)
	}

	return port
}

/////////////////////
/// portPublisher ///
/////////////////////

func (pp *portPublisher) updateEndpoints(endpoints *corev1.Endpoints) {
	newPods := pp.endpointsToAddresses(endpoints)
	if len(newPods) == 0 {
		pp.exists = true
		for _, listener := range pp.listeners {
			listener.NoEndpoints(true)
		}
	} else {
		add, remove := diffPods(pp.pods, newPods)
		pp.exists = true
		pp.pods = newPods
		for _, listener := range pp.listeners {
			if len(remove) > 0 {
				listener.Remove(remove)
			}
			if len(add) > 0 {
				listener.Add(add)
			}
		}
	}
}

func (pp *portPublisher) endpointsToAddresses(endpoints *corev1.Endpoints) PodSet {
	pods := make(PodSet)
	for _, subset := range endpoints.Subsets {
		resolvedPort := pp.resolveTargetPort(&subset)
		for _, endpoint := range subset.Addresses {
			if endpoint.TargetRef.Kind == "Pod" {
				id := PodID{
					Name:      endpoint.TargetRef.Name,
					Namespace: endpoint.TargetRef.Namespace,
				}
				pod, err := pp.k8sAPI.Pod().Lister().Pods(id.Namespace).Get(id.Name)
				if err != nil {
					pp.log.Errorf("Unable to fetch pod %v: %s", id, err)
				}
				ownerName, ownerKind := pp.k8sAPI.GetOwnerKindAndName(pod)
				pods[id] = Address{
					Ip:        endpoint.IP,
					Port:      resolvedPort,
					Pod:       pod,
					OwnerName: ownerName,
					OwnerKind: ownerKind,
				}
			}
		}
	}
	return pods
}

func (pp *portPublisher) resolveTargetPort(subset *corev1.EndpointSubset) Port {
	switch pp.targetPort.Type {
	case intstr.Int:
		return Port(pp.targetPort.IntVal)
	case intstr.String:
		for _, p := range subset.Ports {
			if p.Name == pp.targetPort.StrVal {
				return Port(p.Port)
			}
		}
	}
	return Port(0)
}

func (pp *portPublisher) updatePort(targetPort namedPort) {
	pp.targetPort = targetPort
	endpoints, err := pp.k8sAPI.Endpoint().Lister().Endpoints(pp.id.Namespace).Get(pp.id.Name)
	if err == nil {
		pp.updateEndpoints(endpoints)
	} else {
		pp.noEndpoints(false)
	}
}

func (pp *portPublisher) noEndpoints(exists bool) {
	pp.exists = exists
	for _, listener := range pp.listeners {
		listener.NoEndpoints(exists)
	}
}

func (pp *portPublisher) subscribe(listener EndpointUpdateListener) {
	if pp.exists {
		if len(pp.pods) > 0 {
			listener.Add(pp.pods)
		} else {
			listener.NoEndpoints(true)
		}
	} else {
		listener.NoEndpoints(false)
	}
	pp.listeners = append(pp.listeners, listener)
}

func (pp *portPublisher) unsubscribe(listener EndpointUpdateListener) {
	for i, e := range pp.listeners {
		if e == listener {
			n := len(pp.listeners)
			pp.listeners[i] = pp.listeners[n-1]
			pp.listeners = pp.listeners[:n-1]
			return
		}
	}
}

////////////
/// util ///
////////////

// getTargetPort returns the port specified as an argument if no service is
// present. If the service is present and it has a port spec matching the
// specified port and a target port configured, it returns the name of the
// service's port (not the name of the target pod port), so that it can be
// looked up in the the endpoints API response, which uses service port names.
func getTargetPort(service *corev1.Service, port Port) namedPort {
	// Use the specified port as the target port by default
	targetPort := intstr.FromInt(int(port))

	if service == nil {
		return targetPort
	}

	// If a port spec exists with a port matching the specified port and a target
	// port configured, use that port spec's name as the target port
	for _, portSpec := range service.Spec.Ports {
		if portSpec.Port == int32(port) && portSpec.TargetPort != intstr.FromInt(0) {
			return intstr.FromString(portSpec.Name)
		}
	}

	return targetPort
}

func diffPods(old, new PodSet) (add, remove PodSet) {
	// TODO: this detects pods which have been added or removed, but does not
	// detect pods which have been modified.  A modified pod should trigger
	// an add of the new version.
	add = make(PodSet)
	remove = make(PodSet)
	for id, pod := range new {
		if _, ok := old[id]; !ok {
			add[id] = pod
		}
	}
	for id, pod := range old {
		if _, ok := new[id]; !ok {
			remove[id] = pod
		}
	}
	return
}
