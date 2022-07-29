/*
Copyright (C) 2022 Traefik Labs

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

package main

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ettle/strcase"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/admission"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/admission/ingclass"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/admission/reviewer"
	hubclientset "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/hub/clientset/versioned"
	hubinformer "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/hub/informers/externalversions"
	traefikclientset "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/traefik/clientset/versioned"
	"github.com/traefik/hub-agent-kubernetes/pkg/edgeingress"
	edgeadmission "github.com/traefik/hub-agent-kubernetes/pkg/edgeingress/admission"
	"github.com/traefik/hub-agent-kubernetes/pkg/kube"
	"github.com/traefik/hub-agent-kubernetes/pkg/kubevers"
	"github.com/traefik/hub-agent-kubernetes/pkg/platform"
	"github.com/urfave/cli/v2"
	netv1 "k8s.io/api/networking/v1"
	kerror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	flagACPServerListenAddr     = "acp-server.listen-addr"
	flagACPServerCertificate    = "acp-server.cert"
	flagACPServerKey            = "acp-server.key"
	flagACPServerAuthServerAddr = "acp-server.auth-server-addr"
	flagIngressClassName        = "ingress-class-name"
	flagTraefikEntryPoint       = "traefik.entryPoint"
)

func acpFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    flagACPServerListenAddr,
			Usage:   "Address on which the access control policy server listens for admission requests",
			EnvVars: []string{strcase.ToSNAKE(flagACPServerListenAddr)},
			Value:   "0.0.0.0:443",
		},
		&cli.StringFlag{
			Name:    flagACPServerCertificate,
			Usage:   "Certificate used for TLS by the ACP server",
			EnvVars: []string{strcase.ToSNAKE(flagACPServerCertificate)},
			Value:   "/var/run/hub-agent-kubernetes/cert.pem",
		},
		&cli.StringFlag{
			Name:    flagACPServerKey,
			Usage:   "Key used for TLS by the ACP server",
			EnvVars: []string{strcase.ToSNAKE(flagACPServerKey)},
			Value:   "/var/run/hub-agent-kubernetes/key.pem",
		},
		&cli.StringFlag{
			Name:    flagACPServerAuthServerAddr,
			Usage:   "Address the ACP server can reach the auth server on",
			EnvVars: []string{strcase.ToSNAKE(flagACPServerAuthServerAddr)},
			Value:   "http://hub-agent-auth-server.hub.svc.cluster.local",
		},
		&cli.StringFlag{
			Name:    flagIngressClassName,
			Usage:   "The ingress class name used for ingresses managed by Hub",
			EnvVars: []string{strcase.ToSNAKE(flagIngressClassName)},
		},
		&cli.StringFlag{
			Name:    flagTraefikEntryPoint,
			Usage:   "The entry point used by Traefik to expose tunnels",
			EnvVars: []string{strcase.ToSNAKE(flagTraefikEntryPoint)},
			Value:   "traefikhub-tunl",
		},
	}
}

func webhookAdmission(ctx context.Context, cliCtx *cli.Context, platformClient *platform.Client) error {
	var (
		listenAddr     = cliCtx.String(flagACPServerListenAddr)
		certFile       = cliCtx.String(flagACPServerCertificate)
		keyFile        = cliCtx.String(flagACPServerKey)
		authServerAddr = cliCtx.String(flagACPServerAuthServerAddr)
	)

	if _, err := url.Parse(authServerAddr); err != nil {
		return fmt.Errorf("invalid auth server address: %w", err)
	}

	ingressClassName := cliCtx.String(flagIngressClassName)
	traefikEntryPoint := cliCtx.String(flagTraefikEntryPoint)
	acpAdmission, edgeIngressAdmission, err := setupAdmissionHandlers(ctx, platformClient, authServerAddr, ingressClassName, traefikEntryPoint)
	if err != nil {
		return fmt.Errorf("create admission handler: %w", err)
	}

	webAdmissionACP := admission.NewACPHandler(platformClient)

	router := chi.NewRouter()
	router.Handle("/edge-ingress", edgeIngressAdmission)
	router.Handle("/ingress", acpAdmission)
	router.Handle("/acp", webAdmissionACP)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           router,
		ErrorLog:          stdlog.New(log.Logger.Level(zerolog.DebugLevel), "", 0),
		ReadHeaderTimeout: 2 * time.Second,
	}
	srvDone := make(chan struct{})

	go func() {
		log.Info().Str("addr", listenAddr).Msg("Starting admission server")
		if err = server.ListenAndServeTLS(certFile, keyFile); !errors.Is(err, http.ErrServerClosed) {
			log.Err(err).Msg("Unable to listen and serve admission requests")
		}
		close(srvDone)
	}()

	select {
	case <-ctx.Done():
		gracefulCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err = server.Shutdown(gracefulCtx); err != nil {
			log.Error().Err(err).Msg("Failed to shutdown admission server gracefully")
			if err = server.Close(); err != nil {
				return fmt.Errorf("close admission server: %w", err)
			}
		}
		log.Info().Msg("Successfully shutdown admission server")
	case <-srvDone:
		return errors.New("admission server stopped")
	}

	return nil
}

func setupAdmissionHandlers(ctx context.Context, platformClient *platform.Client, authServerAddr, ingressClassName, traefikEntryPoint string) (acpHdl, edgeIngressHdl http.Handler, err error) {
	config, err := kube.InClusterConfigWithRetrier(2)
	if err != nil {
		return nil, nil, fmt.Errorf("create Kubernetes in-cluster configuration: %w", err)
	}

	clientSet, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create Kubernetes client set: %w", err)
	}

	if ingressClassName == "" {
		ingressClassName = "traefik-hub"
		if err = initIngressClass(ctx, clientSet, ingressClassName); err != nil {
			return nil, nil, fmt.Errorf("initatilize ingressClass: %w", err)
		}
	}

	hubClientSet, err := hubclientset.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create Hub client set: %w", err)
	}

	kubeVers, err := clientSet.Discovery().ServerVersion()
	if err != nil {
		return nil, nil, fmt.Errorf("detect Kubernetes version: %w", err)
	}

	kubeInformer := informers.NewSharedInformerFactory(clientSet, 5*time.Minute)
	hubInformer := hubinformer.NewSharedInformerFactory(hubClientSet, 5*time.Minute)

	ingressUpdater := admission.NewIngressUpdater(kubeInformer, clientSet, kubeVers.GitVersion)

	go ingressUpdater.Run(ctx)

	acpEventHandler := admission.NewEventHandler(ingressUpdater)
	ingClassWatcher := ingclass.NewWatcher()

	err = startKubeInformer(ctx, kubeVers.GitVersion, kubeInformer, ingClassWatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("start kube informer: %w", err)
	}

	hubInformer.Hub().V1alpha1().IngressClasses().Informer().AddEventHandler(ingClassWatcher)
	hubInformer.Hub().V1alpha1().AccessControlPolicies().Informer().AddEventHandler(acpEventHandler)
	hubInformer.Hub().V1alpha1().EdgeIngresses().Informer()

	hubInformer.Start(ctx.Done())

	for t, ok := range hubInformer.WaitForCacheSync(ctx.Done()) {
		if !ok {
			return nil, nil, fmt.Errorf("wait for Hub informer cache sync: %s: %w", t, ctx.Err())
		}
	}

	acpWatcher := acp.NewWatcher(time.Minute, platformClient, hubClientSet, hubInformer)
	go func() {
		acpWatcher.Run(ctx)
	}()

	traefikClientSet, err := traefikclientset.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create Traefik client set: %w", err)
	}

	watcherCfg := edgeingress.WatcherConfig{
		IngressClassName:        ingressClassName,
		TraefikEntryPoint:       traefikEntryPoint,
		AgentNamespace:          currentNamespace(),
		EdgeIngressSyncInterval: time.Minute,
		CertRetryInterval:       time.Minute,
		CertSyncInterval:        time.Hour,
	}
	edgeIngressWatcher, err := edgeingress.NewWatcher(platformClient, hubClientSet, clientSet, traefikClientSet.TraefikV1alpha1(), hubInformer, watcherCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create edge ingress watcher: %w", err)
	}
	go func() {
		edgeIngressWatcher.Run(ctx)
	}()

	polGetter := reviewer.NewPolGetter(hubInformer)

	fwdAuthMdlwrs := reviewer.NewFwdAuthMiddlewares(authServerAddr, polGetter, traefikClientSet.TraefikV1alpha1())

	reviewers := []admission.Reviewer{
		reviewer.NewTraefikIngress(ingClassWatcher, fwdAuthMdlwrs),
	}

	return admission.NewHandler(reviewers), edgeadmission.NewHandler(platformClient), nil
}

func startKubeInformer(ctx context.Context, kubeVers string, kubeInformer informers.SharedInformerFactory, ingClassEventHandler cache.ResourceEventHandler) error {
	if kubevers.SupportsNetV1IngressClasses(kubeVers) {
		kubeInformer.Networking().V1().IngressClasses().Informer().AddEventHandler(ingClassEventHandler)
	} else if kubevers.SupportsNetV1Beta1IngressClasses(kubeVers) {
		kubeInformer.Networking().V1beta1().IngressClasses().Informer().AddEventHandler(ingClassEventHandler)
	}

	if kubevers.SupportsNetV1Ingresses(kubeVers) {
		kubeInformer.Networking().V1().Ingresses().Informer()
	} else {
		// Since we only support Kubernetes v1.14 and up, we should always at least have net v1beta1 Ingresses.
		kubeInformer.Networking().V1beta1().Ingresses().Informer()
	}

	kubeInformer.Start(ctx.Done())

	for t, ok := range kubeInformer.WaitForCacheSync(ctx.Done()) {
		if !ok {
			return fmt.Errorf("wait for cache Kubernetes sync: %s: %w", t, ctx.Err())
		}
	}

	return nil
}

func initIngressClass(ctx context.Context, clientSet clientset.Interface, ingressClassName string) error {
	ic := &netv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: ingressClassName,
		},
		Spec: netv1.IngressClassSpec{
			Controller: "traefik.io/ingress-controller",
		},
	}
	if _, err := clientSet.NetworkingV1().IngressClasses().Create(ctx, ic, metav1.CreateOptions{}); err != nil {
		if !kerror.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

func currentNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}

	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(data)); ns != "" {
			return ns
		}
	}

	return "default"
}
