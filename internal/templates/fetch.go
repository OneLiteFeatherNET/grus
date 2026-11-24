package templates

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// fetchAttempts is one try plus five retries per template.
	fetchAttempts = 6
	fetchBackoff  = 500 * time.Millisecond
	fetchErrBody  = 512
)

// FetchOptions configures Fetch. Client and Sleep default to http.DefaultClient and time.Sleep.
type FetchOptions struct {
	BaseURL   string
	Token     string
	Dest      string
	Templates []Ref
	Client    *http.Client
	Sleep     func(time.Duration)
}

// Ref names one template version.
type Ref struct {
	Name   string
	Digest string
}

// Fetch downloads each template version from the controller and extracts them into dest in order (overlay).
// GET {baseURL}/v1/templates/{name}/versions/{digest}/archive with "Authorization: Bearer <token>".
// Each download is retried up to five times with backoff via the injected sleep func.
func Fetch(ctx context.Context, o FetchOptions) error {
	if o.Dest == "" {
		return errors.New("templates: fetch needs a destination")
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	sleep := o.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for _, ref := range o.Templates {
		if err := validIdent("template name", ref.Name); err != nil {
			return err
		}
		if !digestRe.MatchString(ref.Digest) {
			return fmt.Errorf("templates: invalid digest %q", ref.Digest)
		}
		u := fetchURL(o.BaseURL, ref)
		var err error
		for attempt := 1; attempt <= fetchAttempts; attempt++ {
			if attempt > 1 {
				sleep(fetchBackoff * time.Duration(1<<(attempt-2)))
			}
			err = fetchOnce(ctx, client, u, o.Token, o.Dest)
			if err == nil {
				break
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}
		if err != nil {
			return fmt.Errorf("templates: fetch %s@%s: %w", ref.Name, ref.Digest, err)
		}
	}
	return nil
}

// ParseRefs parses "name@sha256:abc,name2@sha256:def" (the GRUS_FETCH_TEMPLATES format).
// An empty string yields no refs.
func ParseRefs(s string) ([]Ref, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var refs []Ref
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		name, digest, ok := strings.Cut(part, "@")
		if !ok {
			return nil, fmt.Errorf("templates: ref %q: want name@sha256:<hex>", part)
		}
		if err := validIdent("template name", name); err != nil {
			return nil, err
		}
		if !digestRe.MatchString(digest) {
			return nil, fmt.Errorf("templates: ref %q: invalid digest %q", part, digest)
		}
		refs = append(refs, Ref{Name: name, Digest: digest})
	}
	return refs, nil
}

func fetchURL(baseURL string, ref Ref) string {
	return strings.TrimSuffix(baseURL, "/") + "/v1/templates/" + url.PathEscape(ref.Name) +
		"/versions/" + url.PathEscape(ref.Digest) + "/archive"
}

func fetchOnce(ctx context.Context, client *http.Client, u, token, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, fetchErrBody))
		return fmt.Errorf("status %d: %s", resp.StatusCode, snippet)
	}
	return ExtractTarGz(resp.Body, dest)
}
