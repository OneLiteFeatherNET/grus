package main

import (
	"context"
	"fmt"
	"log/slog"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/OneLiteFeatherNET/grus/internal/kube/crdsync"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// newCRDSyncer builds the custom resource syncer on the in-cluster config. It watches only the controller's
// own namespace, so the chart needs a namespaced Role and no ClusterRole.
func newCRDSyncer(cfg config, s *stack, logger *slog.Logger) (*crdsync.Syncer, error) {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	client, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	return crdsync.New(client, s.ctrl, crdsync.Config{
		Namespace: cfg.Namespace,
		Clock:     model.SystemClock{},
		Logger:    logger,
	}), nil
}

// startCRDSync runs the syncer in the background when GRUS_CRDS is set. A failure is logged and does not stop
// the controller: the REST API and the api-owned declarations keep working without the custom resources.
func startCRDSync(ctx context.Context, cfg config, s *stack, logger *slog.Logger) {
	if !cfg.CRDs {
		return
	}
	syncer, err := newCRDSyncer(cfg, s, logger)
	if err != nil {
		logger.Error("custom resource sync disabled", "error", err)
		return
	}
	go func() {
		if err := syncer.Run(ctx); err != nil {
			logger.Error("custom resource sync stopped", "error", err)
		}
	}()
	logger.Info("custom resource sync started", "namespace", cfg.Namespace)
}
