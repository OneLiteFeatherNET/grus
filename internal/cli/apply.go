package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/OneLiteFeatherNET/grus/internal/client"
)

// apiVersion is the only manifest version the CLI accepts; it matches the CRDs.
const apiVersion = "grus.onelitefeather.net/v1alpha1"

// manifestKinds maps manifest kinds to API resources. The order is the apply order: what a pool
// refers to comes first.
var manifestKinds = []struct{ kind, resource string }{
	{"Environment", "environments"},
	{"Group", "groups"},
	{"Pool", "pools"},
}

type manifest struct {
	kind     string
	resource string
	name     string
	spec     map[string]any
	source   string
}

// parseManifests decodes every YAML or JSON document of r. A document of kind List expands to its items.
func parseManifests(source string, r io.Reader) ([]manifest, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var out []manifest
	for n := 1; ; n++ {
		var doc map[string]any
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("%s: document %d: %w", source, n, err)
		}
		if doc == nil {
			continue
		}
		docs := []map[string]any{doc}
		if doc["kind"] == "List" {
			items, _ := doc["items"].([]any)
			docs = docs[:0]
			for _, it := range items {
				m, ok := it.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s: document %d: list item is not an object", source, n)
				}
				docs = append(docs, m)
			}
		}
		for _, d := range docs {
			m, err := toManifest(d)
			if err != nil {
				return nil, fmt.Errorf("%s: document %d: %w", source, n, err)
			}
			m.source = source
			out = append(out, m)
		}
	}
	return out, nil
}

func toManifest(d map[string]any) (manifest, error) {
	if v, _ := d["apiVersion"].(string); v != apiVersion {
		return manifest{}, fmt.Errorf("apiVersion must be %s", apiVersion)
	}
	k, _ := d["kind"].(string)
	m := manifest{kind: k}
	for _, mk := range manifestKinds {
		if mk.kind == k {
			m.resource = mk.resource
		}
	}
	if m.resource == "" {
		return manifest{}, fmt.Errorf("unsupported kind %q, use Environment, Group or Pool", k)
	}
	meta, _ := d["metadata"].(map[string]any)
	m.name, _ = meta["name"].(string)
	if m.name == "" {
		return manifest{}, errors.New("metadata.name is required")
	}
	spec, ok := d["spec"].(map[string]any)
	if !ok {
		return manifest{}, errors.New("spec is required and must be an object")
	}
	m.spec = spec
	return m, nil
}

func rank(resource string) int {
	for i, mk := range manifestKinds {
		if mk.resource == resource {
			return i
		}
	}
	return len(manifestKinds)
}

func newApplyCmd(env *Env, o *opts) *cobra.Command {
	var files []string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "apply -f <file|->",
		Short: "Create or update Pools, Groups and Environments from YAML or JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(files) == 0 {
				return errors.New("-f is required")
			}
			var all []manifest
			for _, f := range files {
				var r io.Reader
				if f == "-" {
					r = cmd.InOrStdin()
				} else {
					fh, err := os.Open(f)
					if err != nil {
						return err
					}
					defer fh.Close()
					r = fh
				}
				ms, err := parseManifests(f, r)
				if err != nil {
					return err
				}
				all = append(all, ms...)
			}
			if len(all) == 0 {
				return errors.New("no resources found in the given files")
			}
			sort.SliceStable(all, func(i, j int) bool { return rank(all[i].resource) < rank(all[j].resource) })
			var cl *client.Client
			if !dryRun {
				var err error
				if cl, _, err = o.client(env); err != nil {
					return err
				}
			}
			for _, m := range all {
				label := strings.ToLower(m.kind) + "/" + m.name
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "%s valid (dry run)\n", label)
					continue
				}
				if _, err := cl.Put(cmdContext(cmd), m.resource, m.name, m.spec); err != nil {
					return fmt.Errorf("%s (%s): %w", label, m.source, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s applied\n", label)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&files, "filename", "f", nil, "manifest file (YAML or JSON), - for stdin; repeatable")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate the manifests locally without calling the controller")
	return cmd
}
