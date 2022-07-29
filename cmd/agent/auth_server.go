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
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/auth"
	hubclientset "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/hub/clientset/versioned"
	hubinformer "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/hub/informers/externalversions"
	"github.com/traefik/hub-agent-kubernetes/pkg/kube"
	"github.com/traefik/hub-agent-kubernetes/pkg/logger"
	"github.com/traefik/hub-agent-kubernetes/pkg/version"
	"github.com/urfave/cli/v2"
)

type authServerCmd struct {
	flags []cli.Flag
}

func newAuthServerCmd() authServerCmd {
	flgs := []cli.Flag{
		&cli.StringFlag{
			Name:    "listen-addr",
			Usage:   "Address on which the auth server listens for auth requests",
			EnvVars: []string{"AUTH_SERVER_LISTEN_ADDR"},
			Value:   "0.0.0.0:80",
		},
	}

	flgs = append(flgs, globalFlags()...)

	return authServerCmd{
		flags: flgs,
	}
}

func (c authServerCmd) build() *cli.Command {
	return &cli.Command{
		Name:   "auth-server",
		Usage:  "Runs the Hub agent authentication server",
		Flags:  c.flags,
		Action: c.run,
	}
}

func (c authServerCmd) run(cliCtx *cli.Context) error {
	logger.Setup(cliCtx.String("log-level"), cliCtx.String("log-format"))

	version.Log()

	config, err := kube.InClusterConfigWithRetrier(2)
	if err != nil {
		return fmt.Errorf("create Kubernetes in-cluster configuration: %w", err)
	}

	hubClientSet, err := hubclientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create Hub client set: %w", err)
	}

	switcher := auth.NewHandlerSwitcher()
	acpWatcher := auth.NewWatcher(switcher)

	hubInformer := hubinformer.NewSharedInformerFactory(hubClientSet, 5*time.Minute)
	hubInformer.Hub().V1alpha1().AccessControlPolicies().Informer().AddEventHandler(acpWatcher)
	hubInformer.Start(cliCtx.Context.Done())

	for t, ok := range hubInformer.WaitForCacheSync(cliCtx.Context.Done()) {
		if !ok {
			return fmt.Errorf("wait for cache sync: %s: %w", t, cliCtx.Context.Err())
		}
	}

	go acpWatcher.Run(cliCtx.Context)

	listenAddr := cliCtx.String("listen-addr")

	mux := http.NewServeMux()

	mux.Handle("/_live", http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	mux.Handle("/_ready", http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))

	mux.Handle("/", switcher)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ErrorLog:          stdlog.New(log.Logger.Level(zerolog.DebugLevel), "", 0),
		ReadHeaderTimeout: 2 * time.Second,
	}

	srvDone := make(chan struct{})

	go func() {
		log.Info().Str("addr", listenAddr).Msg("Starting auth server")
		if err = server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Err(err).Msg("Unable to listen and serve auth requests")
		}
		close(srvDone)
	}()

	select {
	case <-cliCtx.Context.Done():
		gracefulCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err = server.Shutdown(gracefulCtx); err != nil {
			log.Error().Err(err).Msg("Failed to shutdown auth server gracefully")
			if err = server.Close(); err != nil {
				return fmt.Errorf("close auth server: %w", err)
			}
		}
	case <-srvDone:
		return errors.New("auth server stopped")
	}

	return nil
}
