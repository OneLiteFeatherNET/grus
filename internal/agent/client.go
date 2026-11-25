package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver/remote"
)

const (
	requestTimeout = 15 * time.Second
	errBodyLimit   = 256
)

// Client talks to the controller's agent routes. It implements API.
type Client struct {
	base  string
	host  string
	token string
	http  *http.Client
}

var _ API = (*Client)(nil)

// NewHTTPClient returns an HTTP client that trusts the system CAs plus the PEM file at caFile, if given.
// It has no overall timeout: every call carries its own context deadline, the long poll included.
func NewHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA file %s holds no certificate", caFile)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: transport}, nil
}

func (c *Client) path(suffix string) string {
	return c.base + "/v1/agents/" + url.PathEscape(c.host) + suffix
}

func (c *Client) Register(ctx context.Context, req remote.RegisterRequest) error {
	return c.do(ctx, requestTimeout, http.MethodPut, c.path(""), req, nil)
}

func (c *Client) Desired(ctx context.Context, revision int64, wait time.Duration) (remote.Desired, error) {
	q := url.Values{"wait": {wait.String()}, "revision": {strconv.FormatInt(revision, 10)}}
	var out remote.Desired
	err := c.do(ctx, wait+requestTimeout, http.MethodGet, c.path("/desired")+"?"+q.Encode(), nil, &out)
	return out, err
}

func (c *Client) Observed(ctx context.Context, rep remote.ObservedReport) error {
	return c.do(ctx, requestTimeout, http.MethodPut, c.path("/observed"), rep, nil)
}

// do sends one JSON request. The token only travels in the Authorization header and never ends up in an error.
func (c *Client) do(ctx context.Context, timeout time.Duration, method, target string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
		return fmt.Errorf("%s %s: HTTP %d: %s", method, req.URL.Path, resp.StatusCode, snippet)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, req.URL.Path, err)
	}
	return nil
}
