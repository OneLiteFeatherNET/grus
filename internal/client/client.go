// Package client is a small typed HTTP client for the Grus REST API. It is what the grus CLI talks to.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// requestTimeout bounds every non-streaming call.
const requestTimeout = 30 * time.Second

// Object is a decoded API resource. The CLI keeps resources generic so fields added by newer
// controllers show up without a client change.
type Object = map[string]any

// optional lists the resources an older controller may not serve; a 404 on them means "not supported".
var optional = map[string]bool{"groups": true, "environments": true, "hosts": true}

// APIError is an error answer of the controller (RFC 9457 problem document).
type APIError struct {
	Status int
	Title  string
	Detail string
	Errors []string
}

func (e *APIError) Error() string {
	msg := e.Detail
	if msg == "" {
		msg = e.Title
	}
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if len(e.Errors) > 0 {
		msg += ": " + strings.Join(e.Errors, "; ")
	}
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		msg += " (check the token, see grus login)"
	}
	return fmt.Sprintf("controller answered %d: %s", e.Status, msg)
}

// NotSupportedError means the controller does not serve the endpoint (it answered 404 for a list route).
type NotSupportedError struct{ What string }

func (e *NotSupportedError) Error() string { return e.What + " is not supported by this controller" }

// Client talks to one controller.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New returns a client for the controller at base. A nil hc uses a client without an overall timeout;
// non-streaming calls are bounded by a context deadline instead.
func New(base, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{base: strings.TrimRight(base, "/"), token: token, http: hc}
}

func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, body any) (*http.Request, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// send performs the request and returns the response; a status >= 400 becomes an *APIError.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, decodeProblem(resp)
	}
	return resp, nil
}

func decodeProblem(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var p struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Errors []struct {
			Message  string `json:"message"`
			Location string `json:"location"`
		} `json:"errors"`
	}
	e := &APIError{Status: resp.StatusCode}
	if json.Unmarshal(raw, &p) == nil {
		e.Title, e.Detail = p.Title, p.Detail
		for _, d := range p.Errors {
			if d.Location != "" {
				e.Errors = append(e.Errors, d.Location+": "+d.Message)
			} else {
				e.Errors = append(e.Errors, d.Message)
			}
		}
	}
	return e
}

// call is a bounded JSON round trip; out may be nil.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := c.newRequest(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func isStatus(err error, codes ...int) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	for _, c := range codes {
		if ae.Status == c {
			return true
		}
	}
	return false
}

// List returns every object of a resource: pools, instances, groups, environments or hosts.
// Instances accept the filters pool, state and role in query.
func (c *Client) List(ctx context.Context, resource string, query url.Values) ([]Object, error) {
	var out []Object
	err := c.call(ctx, http.MethodGet, "/v1/"+resource, query, nil, &out)
	if err != nil {
		if optional[resource] && isStatus(err, http.StatusNotFound) {
			return nil, &NotSupportedError{What: resource}
		}
		return nil, err
	}
	return out, nil
}

// Get returns one object by name (for instances: by id).
func (c *Client) Get(ctx context.Context, resource, name string) (Object, error) {
	var out Object
	err := c.call(ctx, http.MethodGet, "/v1/"+resource+"/"+url.PathEscape(name), nil, nil, &out)
	if err != nil && optional[resource] && isStatus(err, http.StatusNotFound) {
		// A 404 is either an unknown name or a missing route; the list route tells them apart.
		if _, lerr := c.List(ctx, resource, nil); lerr != nil {
			var ns *NotSupportedError
			if errors.As(lerr, &ns) {
				return nil, lerr
			}
		}
	}
	return out, err
}

// Put creates or replaces a pool, group or environment from its spec.
func (c *Client) Put(ctx context.Context, resource, name string, spec any) (Object, error) {
	var out Object
	err := c.call(ctx, http.MethodPut, "/v1/"+resource+"/"+url.PathEscape(name), nil, spec, &out)
	if err != nil && optional[resource] && isStatus(err, http.StatusNotFound, http.StatusMethodNotAllowed) {
		return nil, &NotSupportedError{What: resource}
	}
	return out, err
}

// Templates returns template name -> tag -> digest.
func (c *Client) Templates(ctx context.Context) (map[string]map[string]string, error) {
	var out map[string]map[string]string
	err := c.call(ctx, http.MethodGet, "/v1/templates", nil, nil, &out)
	return out, err
}

// Network returns the network-wide player count.
func (c *Client) Network(ctx context.Context) (Object, error) {
	var out Object
	err := c.call(ctx, http.MethodGet, "/v1/network", nil, nil, &out)
	if err != nil && isStatus(err, http.StatusNotFound) {
		return nil, &NotSupportedError{What: "network"}
	}
	return out, err
}

// StopInstance stops an instance. With players on it the controller needs drain or force.
func (c *Client) StopInstance(ctx context.Context, id string, drain, force bool) (Object, error) {
	var body any
	if drain || force {
		body = map[string]bool{"drain": drain, "force": force}
	}
	var out Object
	err := c.call(ctx, http.MethodPost, "/v1/instances/"+url.PathEscape(id)+"/stop", nil, body, &out)
	return out, err
}

// Command sends one console line to an instance.
func (c *Client) Command(ctx context.Context, id, line string) error {
	return c.call(ctx, http.MethodPost, "/v1/instances/"+url.PathEscape(id)+"/command", nil,
		map[string]string{"line": line}, nil)
}

// Logs returns the last tail lines of an instance log as text. The caller closes the reader.
func (c *Client) Logs(ctx context.Context, id string, tail int) (io.ReadCloser, error) {
	q := url.Values{}
	if tail > 0 {
		q.Set("tail", fmt.Sprint(tail))
	}
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/instances/"+url.PathEscape(id)+"/logs", q, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// FollowLogs streams the log of an instance and calls fn for every line until ctx ends or the stream closes.
func (c *Client) FollowLogs(ctx context.Context, id string, fn func(line string) error) error {
	return c.stream(ctx, "/v1/instances/"+url.PathEscape(id)+"/logs/stream", "", func(ev Event) error {
		return fn(string(ev.Data))
	})
}

// Event is one server-sent event of GET /v1/events.
type Event struct {
	ID   string
	Type string
	Data json.RawMessage
}

// Events streams the controller events and calls fn for each until ctx ends or the stream closes.
// lastID resumes after that event id when not empty.
func (c *Client) Events(ctx context.Context, lastID string, fn func(Event) error) error {
	return c.stream(ctx, "/v1/events", lastID, fn)
}

func (c *Client) stream(ctx context.Context, path, lastID string, fn func(Event) error) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	err = ReadSSE(resp.Body, fn)
	if ctx.Err() != nil {
		return nil
	}
	return err
}
