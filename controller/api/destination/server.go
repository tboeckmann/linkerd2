package destination

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	discoveryPb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type (
	server struct {
		endpoints *watcher.EndpointsWatcher
		profiles  *watcher.ProfileWatcher

		enableH2Upgrade     bool
		controllerNS        string
		identityTrustDomain string

		log      *logging.Entry
		shutdown <-chan struct{}
	}
)

// NewServer returns a new instance of the destination server.
//
// The destination server serves service discovery and other information to the
// proxy.  This implementation supports the "k8s" destination scheme and expects
// destination paths to be of the form:
// <service>.<namespace>.svc.cluster.local:<port>
//
// If the port is omitted, 80 is used as a default.  If the namespace is
// omitted, "default" is used as a default.append
//
// Addresses for the given destination are fetched from the Kubernetes Endpoints
// API.
func NewServer(
	addr string,
	controllerNS string,
	identityTrustDomain string,
	enableH2Upgrade bool,
	k8sAPI *k8s.API,
	shutdown <-chan struct{},
) *grpc.Server {
	log := logging.WithFields(logging.Fields{
		"addr":      addr,
		"component": "server",
	})
	endpoints := watcher.NewEndpointsWatcher(k8sAPI, log)
	profiles := watcher.NewProfileWatcher(k8sAPI, log)

	srv := server{
		endpoints,
		profiles,
		enableH2Upgrade,
		controllerNS,
		identityTrustDomain,
		log,
		shutdown,
	}

	s := prometheus.NewGrpcServer()
	// linkerd2-proxy-api/destination.Destination (proxy-facing)
	pb.RegisterDestinationServer(s, &srv)
	// controller/discovery.Discovery (controller-facing)
	discoveryPb.RegisterDiscoveryServer(s, &srv)
	return s
}

func (s *server) Get(dest *pb.GetDestination, stream pb.Destination_GetServer) error {
	s.log.Debugf("get %s", dest.GetPath())

	service, port, err := s.getServiceAndPort(dest)
	if err != nil {
		return err
	}

	translator := newEndpointTranslator(
		s.controllerNS,
		s.identityTrustDomain,
		s.enableH2Upgrade,
		service,
		stream,
		s.log,
	)

	s.endpoints.Subscribe(service, port, translator)
	defer s.endpoints.Unsubscribe(service, port, translator)

	select {
	case <-s.shutdown:
	case <-stream.Context().Done():
	}

	return nil
}

func (s *server) GetProfile(dest *pb.GetDestination, stream pb.Destination_GetProfileServer) error {
	s.log.Debugf("GetProfile(%+v)", dest)
	host, _, err := s.getHostAndPort(dest)
	if err != nil {
		return err
	}
	service, _, err := s.getServiceAndPort(dest)
	if err != nil {
		return err
	}

	translator := newProfileTranslator(stream, s.log)

	primary, secondary := newFallbackProfileListener(translator)

	if ns := nsFromToken(dest.GetContextToken()); ns != "" {
		clientProfileID := watcher.ProfileID{
			Namespace: ns,
			Name:      host,
		}

		s.profiles.Subscribe(clientProfileID, primary)
		defer s.profiles.Unsubscribe(clientProfileID, primary)
	}

	serverProfileID := watcher.ProfileID{
		Namespace: service.Namespace,
		Name:      host,
	}

	s.profiles.Subscribe(serverProfileID, secondary)
	defer s.profiles.Unsubscribe(serverProfileID, secondary)

	select {
	case <-s.shutdown:
	case <-stream.Context().Done():
	}

	return nil
}

func nsFromToken(ctx string) string {
	// ns:<namespace>
	parts := strings.Split(ctx, ":")
	if len(parts) == 2 && parts[0] == "ns" {
		return parts[1]
	}

	return ""
}

func (s *server) Endpoints(ctx context.Context, params *discoveryPb.EndpointsParams) (*discoveryPb.EndpointsResponse, error) {
	s.log.Debugf("serving endpoints request")

	// servicePorts := e.getState()

	// rsp := discoveryPb.EndpointsResponse{
	// 	ServicePorts: make(map[string]*discoveryPb.ServicePort),
	// }

	// for serviceID, portMap := range servicePorts {
	// 	discoverySP := discoveryPb.ServicePort{
	// 		PortEndpoints: make(map[uint32]*discoveryPb.PodAddresses),
	// 	}
	// 	for port, sp := range portMap {
	// 		podAddrs := discoveryPb.PodAddresses{
	// 			PodAddresses: []*discoveryPb.PodAddress{},
	// 		}

	// 		for _, ua := range sp.addresses {
	// 			ownerKind, ownerName := s.k8sAPI.GetOwnerKindAndName(ua.pod)
	// 			pod := util.K8sPodToPublicPod(*ua.pod, ownerKind, ownerName)

	// 			podAddrs.PodAddresses = append(
	// 				podAddrs.PodAddresses,
	// 				&discoveryPb.PodAddress{
	// 					Addr: addr.NetToPublic(ua.address),
	// 					Pod:  &pod,
	// 				},
	// 			)
	// 		}

	// 		discoverySP.PortEndpoints[port] = &podAddrs
	// 	}

	// 	s.log.Debugf("ServicePorts[%s]: %+v", serviceID, discoverySP)
	// 	rsp.ServicePorts[serviceID.String()] = &discoverySP
	// }

	return nil, fmt.Errorf("Not implemented;")
}

func (s *server) getHostAndPort(dest *pb.GetDestination) (string, watcher.Port, error) {
	if dest.Scheme != "k8s" {
		err := fmt.Errorf("Unsupported scheme %s", dest.Scheme)
		s.log.Error(err)
		return "", 0, err
	}
	hostPort := strings.Split(dest.Path, ":")
	if len(hostPort) > 2 {
		err := fmt.Errorf("Invalid destination %s", dest.Path)
		s.log.Error(err)
		return "", 0, err
	}
	host := hostPort[0]
	port := 80
	if len(hostPort) == 2 {
		var err error
		port, err = strconv.Atoi(hostPort[1])
		if err != nil {
			return "", 0, fmt.Errorf("Invalid port %s", hostPort[1])
		}
	}
	return host, watcher.Port(port), nil
}

func (s *server) getServiceAndPort(dest *pb.GetDestination) (watcher.ServiceID, watcher.Port, error) {
	if dest.Scheme != "k8s" {
		err := fmt.Errorf("Unsupported scheme %s", dest.Scheme)
		s.log.Error(err)
		return watcher.ServiceID{}, 0, err
	}
	host, port, err := s.getHostAndPort(dest)
	if err != nil {
		return watcher.ServiceID{}, 0, err
	}
	domains := strings.Split(host, ".")
	// S.N.svc.cluster.local
	if len(domains) != 5 {
		return watcher.ServiceID{}, 0, fmt.Errorf("Invalid k8s service %s", host)
	}
	service := watcher.ServiceID{
		Name:      domains[0],
		Namespace: domains[1],
	}
	return service, watcher.Port(port), nil
}
