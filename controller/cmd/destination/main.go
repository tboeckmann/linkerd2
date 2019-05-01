package main

import (
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/api/destination"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/flags"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8086", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9996", "address to serve scrapable metrics on")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	_ = flag.String("kubernetes-dns-zone", "", "The DNS suffix for the local Kubernetes zone.")
	enableH2Upgrade := flag.Bool("enable-h2-upgrade", true, "Enable transparently upgraded HTTP2 connections among pods in the service mesh")
	disableIdentity := flag.Bool("disable-identity", false, "Disable identity configuration")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	flags.ConfigureAndParse()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(
		*kubeConfigPath,
		k8s.Endpoint, k8s.Pod, k8s.RS, k8s.Svc, k8s.SP,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	done := make(chan struct{})

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %s", *addr, err)
	}

	trustDomain := ""
	if *disableIdentity {
		log.Info("Identity is disabled")
	} else {
		global, err := config.Global(consts.MountPathGlobalConfig)
		if err != nil {
			log.Fatalf("Failed to load global config: %s", err)
		}

		trustDomain = global.GetIdentityContext().GetTrustDomain()
	}

	server := destination.NewServer(
		*addr,
		*controllerNamespace,
		trustDomain,
		*enableH2Upgrade,
		k8sAPI,
		done,
	)

	k8sAPI.Sync() // blocks until caches are synced

	go func() {
		log.Infof("starting gRPC server on %s", *addr)
		server.Serve(lis)
	}()

	go admin.StartServer(*metricsAddr)

	<-stop

	log.Infof("shutting down gRPC server on %s", *addr)
	close(done)
	server.GracefulStop()
}
