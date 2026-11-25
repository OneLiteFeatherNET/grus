// Command grus-agent runs Grus instances on a host outside Kubernetes. It pulls the desired instances from
// the controller over HTTPS, runs them as local processes and reports what it observes. It never imports
// client-go.
//
// An agent restart ends its instances (the agent cannot re-attach to them) and starts them again from the
// desired state. A SIGTERM to the agent does not stop instances unless --stop-on-exit is set.
package main

import (
	"context"
	"fmt"
	"github.com/OneLiteFeatherNET/grus/internal/agent"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	processdriver "github.com/OneLiteFeatherNET/grus/internal/driver/process"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/templates"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "grus-agent: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "version" {
		fmt.Println(version)
		return nil
	}
	cfg, err := parseConfig(args, os.Getenv, os.Hostname)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	httpClient, err := agent.NewHTTPClient(cfg.CAFile)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return err
	}
	cfg.Dir = dir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create instance directory: %w", err)
	}
	agent.KillOrphans(dir, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fetch := func(ctx context.Context, refs []driver.ResolvedTemplate, dest string) error {
		list := make([]templates.Ref, len(refs))
		for i, r := range refs {
			list[i] = templates.Ref{Name: r.Name, Digest: r.Digest}
		}
		return templates.Fetch(ctx, templates.FetchOptions{
			BaseURL: cfg.URL, Token: cfg.Token, Dest: dest, Templates: list, Client: httpClient,
		})
	}
	procs := processdriver.New(processdriver.Config{
		HostID:           cfg.Host,
		BaseDir:          dir,
		FetchTemplates:   fetch,
		Logger:           logger,
		Clock:            model.SystemClock{},
		PortMin:          cfg.PortMin,
		PortMax:          cfg.PortMax,
		AdvertiseAddress: cfg.Address,
		JavaPath:         cfg.Java,
	})
	api := agent.NewClient(cfg.URL, cfg.Host, cfg.Token, httpClient)
	a := agent.New(ctx, cfg, api, procs, logger)

	logger.Info("agent started", "host", cfg.Host, "controller", cfg.URL, "dir", dir, "version", version)
	a.Run(ctx)
	if cfg.StopOnExit {
		logger.Info("stopping instances before exit")
		a.StopAll(context.Background())
	}
	logger.Info("agent stopped")
	return nil
}
