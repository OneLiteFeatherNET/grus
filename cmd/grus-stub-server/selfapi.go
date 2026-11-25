package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// selfClient calls the controller's self endpoints with the instance token.
type selfClient struct {
	base  string
	token string
	http  *http.Client
}

func newSelfClient(base, token string, hc *http.Client) *selfClient {
	return &selfClient{base: base, token: token, http: hc}
}

// Ready reports that the instance accepts connections (POST /v1/self/ready).
func (c *selfClient) Ready(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/self/ready", nil)
}

// Status reports the player count and doubles as heartbeat (PUT /v1/self/status).
func (c *selfClient) Status(ctx context.Context, players, maxPlayers int) error {
	body, err := json.Marshal(struct {
		Players    int `json:"players"`
		MaxPlayers int `json:"maxPlayers"`
	}{players, maxPlayers})
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPut, "/v1/self/status", body)
}

// Shutdown asks the controller to stop this instance (POST /v1/self/shutdown).
func (c *selfClient) Shutdown(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/self/shutdown", nil)
}

func (c *selfClient) do(ctx context.Context, method, path string, body []byte) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: status %d", method, path, resp.StatusCode)
	}
	return nil
}

// reporter drives the self API: ready once, then one status report per tick.
type reporter struct {
	client     *selfClient
	maxPlayers int
	ready      bool
	log        *slog.Logger
}

// Tick performs one reporting step. Until the controller accepts ready, Tick retries it and sends no
// status. The stub never has players, so the status always reports zero.
func (r *reporter) Tick(ctx context.Context) error {
	if !r.ready {
		if err := r.client.Ready(ctx); err != nil {
			return fmt.Errorf("ready: %w", err)
		}
		r.ready = true
	}
	return r.client.Status(ctx, 0, r.maxPlayers)
}

// Run reports immediately and then once for every value received on ticks. The caller owns the ticker,
// so tests drive the loop without waiting. It returns when ctx is done.
func (r *reporter) Run(ctx context.Context, ticks <-chan time.Time) {
	r.report(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			r.report(ctx)
		}
	}
}

func (r *reporter) report(ctx context.Context) {
	if err := r.Tick(ctx); err != nil && ctx.Err() == nil {
		r.log.Warn("grus self report failed", "error", err)
	}
}
