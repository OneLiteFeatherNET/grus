// Command grus-stub-server is a native, non-JVM example game server for Grus. It answers the Minecraft
// Java server list (status and ping), refuses logins with a readable message, and, when GRUS_URL and
// GRUS_TOKEN are set, reports to the Grus self API. It proves that Grus runs servers that are not JVM
// processes. See the README for the environment variables.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/mcproto"
)

const (
	statusInterval  = 5 * time.Second
	connDeadline    = 30 * time.Second
	shutdownTimeout = 5 * time.Second
	versionName     = "grus-stub"
	loginMessage    = "This is a Grus stub server"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("grus-stub-server failed", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", net.JoinHostPort("", cfg.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.Info("listening", "port", cfg.Port, "motd", cfg.MOTD)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	mcCfg := mcproto.Config{VersionName: versionName, MOTD: cfg.MOTD, MaxPlayers: cfg.MaxPlayers, LoginText: loginMessage}
	go acceptLoop(log, ln, mcCfg)

	var client *selfClient
	var reporterDone chan struct{}
	if cfg.selfEnabled() {
		client = newSelfClient(cfg.URL, cfg.Token, &http.Client{Timeout: 10 * time.Second})
		rep := &reporter{client: client, maxPlayers: cfg.MaxPlayers, log: log}
		ticker := time.NewTicker(statusInterval)
		defer ticker.Stop()
		reporterDone = make(chan struct{})
		go func() {
			defer close(reporterDone)
			rep.Run(ctx, ticker.C)
		}()
	}

	<-ctx.Done()
	log.Info("stopping")
	_ = ln.Close()
	if reporterDone != nil {
		<-reporterDone
	}
	if client != nil {
		sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := client.Shutdown(sctx); err != nil {
			log.Warn("grus shutdown request failed", "error", err)
		}
	}
	return nil
}

func acceptLoop(log *slog.Logger, ln net.Listener, cfg mcproto.Config) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Warn("accept failed", "error", err)
			}
			return
		}
		go func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(connDeadline))
			if err := mcproto.Serve(conn, cfg); err != nil {
				log.Debug("connection ended", "remote", conn.RemoteAddr().String(), "error", err)
			}
		}()
	}
}
