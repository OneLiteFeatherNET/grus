package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/OneLiteFeatherNET/grus/internal/api"
	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/driver"
	kubedriver "github.com/OneLiteFeatherNET/grus/internal/driver/kubernetes"
	processdriver "github.com/OneLiteFeatherNET/grus/internal/driver/process"
	"github.com/OneLiteFeatherNET/grus/internal/events"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
	"github.com/OneLiteFeatherNET/grus/internal/templates"
)

const (
	eventRingSize     = 10000
	sseHeartbeat      = 15 * time.Second
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
	seedTag           = "stable"
)

// stack is the assembled controller: every component wired together, not yet reconciling.
type stack struct {
	store   store.Store
	tpl     templates.Store
	drv     driver.Driver
	hub     *events.Hub
	ctrl    *controller.Controller
	handler http.Handler
}

// newStack opens the store, the template store, the driver and the controller and builds the HTTP handler.
// The caller seeds nothing and starts nothing: Run starts the reconcile loop.
func newStack(ctx context.Context, cfg config, logger *slog.Logger) (*stack, error) {
	st, err := store.OpenSQLite(ctx, cfg.DB, model.SystemClock{})
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	tpl, err := openTemplates(cfg)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("open templates: %w", err)
	}
	if cfg.TemplateSeedDir != "" {
		if err := seedTemplates(ctx, tpl, cfg.TemplateSeedDir, logger); err != nil {
			_ = st.Close()
			return nil, fmt.Errorf("seed templates: %w", err)
		}
	}
	drv, err := newDriver(cfg, tpl, logger)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	hub := events.NewHub(strconv.FormatInt(time.Now().UnixMilli(), 10), eventRingSize, model.SystemClock{}, logger)
	ctrl := controller.New(controller.Config{
		PublicURL:    cfg.PublicURL,
		MaxInstances: cfg.MaxInstances,
		Interval:     cfg.ReconcileInterval,
		Logger:       logger,
	}, st, drv, tpl, hub)
	handler := api.New(api.Config{
		AdminToken:     cfg.AdminToken,
		DebugEndpoints: cfg.DebugEndpoints,
		Ready:          ctrl.Ready,
		Version:        version,
	}, ctrl, tpl, hub.Handler(sseHeartbeat), logger)
	return &stack{store: st, tpl: tpl, drv: drv, hub: hub, ctrl: ctrl, handler: handler}, nil
}

// openTemplates opens the template store of the configured backend: fs (default) or s3.
func openTemplates(cfg config) (templates.Store, error) {
	if cfg.TemplatesBackend == "s3" {
		s, err := templates.NewS3(templates.S3Config{
			Endpoint:  cfg.S3Endpoint,
			Bucket:    cfg.S3Bucket,
			Prefix:    cfg.S3Prefix,
			Insecure:  cfg.S3Insecure,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
		})
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	s, err := templates.NewFS(cfg.TemplatesDir)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// runController is the `grus controller` subcommand. SIGINT or SIGTERM stops the HTTP server and the
// reconcile loop. It never stops instances: the next start adopts them.
func runController(args []string) error {
	cfg, err := loadConfig(args, os.Getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := newStack(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := s.store.Close(); err != nil {
			logger.Error("close store", "error", err)
		}
	}()

	server := &http.Server{Addr: cfg.Listen, Handler: s.handler, ReadHeaderTimeout: readHeaderTimeout}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()

	ctrlCtx, cancelCtrl := context.WithCancel(ctx)
	var ctrlErr error
	ctrlExited := make(chan struct{})
	go func() {
		ctrlErr = s.ctrl.Run(ctrlCtx)
		close(ctrlExited)
	}()
	logger.Info("controller started", "listen", cfg.Listen, "driver", s.drv.Name(), "host", s.drv.Host())

	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("http server: %w", err)
		}
	case <-ctrlExited:
		runErr = errors.New("reconcile loop stopped")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", "error", err)
	}
	cancelCtrl()
	<-ctrlExited
	if ctrlErr != nil && runErr == nil {
		runErr = ctrlErr
	}
	logger.Info("stopped")
	return runErr
}

// newDriver builds the configured driver. The kubernetes driver uses the in-cluster config.
func newDriver(cfg config, tpl templates.Store, logger *slog.Logger) (driver.Driver, error) {
	switch cfg.Driver {
	case "kubernetes":
		restCfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
		client, err := kubernetes.NewForConfig(restCfg)
		if err != nil {
			return nil, fmt.Errorf("kubernetes client: %w", err)
		}
		return kubedriver.New(client, kubedriver.Config{
			Namespace:            cfg.Namespace,
			HostID:               cfg.HostID,
			SelfImage:            cfg.SelfImage,
			ImagePullSecrets:     cfg.ImagePullSecrets,
			DefaultPriorityClass: cfg.DefaultPriorityClass,
			NodeSelector:         cfg.NodeSelector,
			PodLabels:            cfg.PodLabels,
			RestConfig:           restCfg,
		}), nil
	case "process":
		return processdriver.New(processdriver.Config{
			HostID:         cfg.HostID,
			BaseDir:        cfg.ProcessDir,
			FetchTemplates: fetchTemplatesTo(tpl),
			Logger:         logger,
			Clock:          model.SystemClock{},
		}), nil
	default:
		return nil, fmt.Errorf("unknown driver %q", cfg.Driver)
	}
}

// fetchTemplatesTo returns the process driver hook that materialises each template version into dir,
// in order, streaming the tar.gz from the template store into the extractor.
func fetchTemplatesTo(tpl templates.Store) func(ctx context.Context, refs []driver.ResolvedTemplate, dir string) error {
	return func(ctx context.Context, refs []driver.ResolvedTemplate, dir string) error {
		for _, ref := range refs {
			pr, pw := io.Pipe()
			go func() { _ = pw.CloseWithError(tpl.WriteTarGz(ctx, ref.Name, ref.Digest, pw)) }()
			if err := templates.ExtractTarGz(pr, dir); err != nil {
				_ = pr.CloseWithError(err)
				return fmt.Errorf("extract %s@%s: %w", ref.Name, ref.Digest, err)
			}
			_ = pr.Close()
		}
		return nil
	}
}

// seedTemplates imports every subdirectory of dir as a template and tags its version as "stable".
func seedTemplates(ctx context.Context, tpl templates.Store, dir string, logger *slog.Logger) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read seed dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		v, err := tpl.ImportDir(ctx, name, filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("import %s: %w", name, err)
		}
		if err := tpl.Tag(ctx, name, seedTag, v.Digest); err != nil {
			return fmt.Errorf("tag %s: %w", name, err)
		}
		logger.Info("template seeded", "name", name, "digest", v.Digest)
	}
	return nil
}
