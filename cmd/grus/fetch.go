package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/templates"
)

// runFetch is the init container entrypoint: it downloads the templates named in GRUS_FETCH_TEMPLATES
// from the controller (GRUS_URL, GRUS_TOKEN) into --dest. Without templates it succeeds at once.
func runFetch(args []string) error {
	fs := flag.NewFlagSet("grus fetch", flag.ContinueOnError)
	dest := fs.String("dest", "", "directory the templates are extracted into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs, err := templates.ParseRefs(os.Getenv("GRUS_FETCH_TEMPLATES"))
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	if *dest == "" {
		return errors.New("--dest is required")
	}
	baseURL := os.Getenv("GRUS_URL")
	if baseURL == "" {
		return errors.New("GRUS_URL is required")
	}
	return templates.Fetch(context.Background(), templates.FetchOptions{
		BaseURL:   baseURL,
		Token:     os.Getenv("GRUS_TOKEN"),
		Dest:      *dest,
		Templates: refs,
		Client:    &http.Client{Timeout: 60 * time.Second},
		Sleep:     time.Sleep,
	})
}
