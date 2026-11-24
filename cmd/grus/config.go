package main

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const minAdminTokenLen = 16

// config is the controller configuration. Environment variables provide the defaults; command line
// flags override them. The admin token has no flag on purpose: flags show up in ps and in usage output.
type config struct {
	Listen               string
	DB                   string
	TemplatesDir         string
	TemplateSeedDir      string
	Driver               string
	Namespace            string
	PublicURL            string
	SelfImage            string
	ImagePullSecrets     []string
	DefaultPriorityClass string
	NodeSelector         map[string]string
	PodLabels            map[string]string
	MaxInstances         int
	DebugEndpoints       bool
	HostID               string
	AdminToken           string
	ProcessDir           string
	ReconcileInterval    time.Duration
}

// loadConfig parses args (the controller flags) on top of the environment read through getenv.
func loadConfig(args []string, getenv func(string) string) (config, error) {
	fs := flag.NewFlagSet("grus controller", flag.ContinueOnError)
	str := func(flagName, env, def, usage string) *string {
		v := getenv(env)
		if v == "" {
			v = def
		}
		return fs.String(flagName, v, usage+" ["+env+"]")
	}
	listen := str("listen", "GRUS_LISTEN", ":8080", "HTTP listen address")
	db := str("db", "GRUS_DB", "./grus.db", "SQLite database file")
	templatesDir := str("templates-dir", "GRUS_TEMPLATES_DIR", "./templates", "template store root")
	seedDir := str("template-seed-dir", "GRUS_TEMPLATE_SEED_DIR", "", "directory of template seeds, one subdirectory per template")
	driverName := str("driver", "GRUS_DRIVER", "kubernetes", "instance driver: kubernetes or process")
	namespace := str("namespace", "GRUS_NAMESPACE", "", "kubernetes namespace of the instances")
	publicURL := str("public-url", "GRUS_PUBLIC_URL", "", "URL instances use to reach the controller (required)")
	selfImage := str("self-image", "GRUS_SELF_IMAGE", "", "grus image used for the template init container")
	pullSecrets := str("image-pull-secrets", "GRUS_IMAGE_PULL_SECRETS", "", "comma list of image pull secrets")
	priority := str("default-priority-class", "GRUS_DEFAULT_PRIORITY_CLASS", "", "priority class when a pool sets none")
	nodeSelector := str("node-selector", "GRUS_NODE_SELECTOR", "", "k=v,k2=v2 node selector for instance pods")
	podLabels := str("pod-labels", "GRUS_POD_LABELS", "", "k=v,k2=v2 extra labels for instance pods")
	maxInstances := str("max-instances", "GRUS_MAX_INSTANCES", "10", "hard limit of non-terminal instances")
	debugEndpoints := str("debug-endpoints", "GRUS_DEBUG_ENDPOINTS", "false", "enable the admin debug endpoints (true/false)")
	hostID := str("host-id", "GRUS_HOST_ID", "", "host id; default k8s-<namespace> or local")
	processDir := str("process-dir", "GRUS_PROCESS_DIR", "./instances", "working directories of the process driver")
	interval := str("reconcile-interval", "GRUS_RECONCILE_INTERVAL", "5s", "reconcile interval")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	cfg := config{
		Listen:               *listen,
		DB:                   *db,
		TemplatesDir:         *templatesDir,
		TemplateSeedDir:      *seedDir,
		Driver:               *driverName,
		Namespace:            *namespace,
		PublicURL:            *publicURL,
		SelfImage:            *selfImage,
		DefaultPriorityClass: *priority,
		ProcessDir:           *processDir,
		AdminToken:           getenv("GRUS_ADMIN_TOKEN"),
		HostID:               *hostID,
	}
	cfg.ImagePullSecrets = splitList(*pullSecrets)

	var err error
	if cfg.NodeSelector, err = parseKeyValues("GRUS_NODE_SELECTOR", *nodeSelector); err != nil {
		return config{}, err
	}
	if cfg.PodLabels, err = parseKeyValues("GRUS_POD_LABELS", *podLabels); err != nil {
		return config{}, err
	}
	if cfg.MaxInstances, err = strconv.Atoi(*maxInstances); err != nil {
		return config{}, fmt.Errorf("GRUS_MAX_INSTANCES: %w", err)
	}
	if cfg.DebugEndpoints, err = strconv.ParseBool(*debugEndpoints); err != nil {
		return config{}, fmt.Errorf("GRUS_DEBUG_ENDPOINTS: %w", err)
	}
	if cfg.ReconcileInterval, err = time.ParseDuration(*interval); err != nil {
		return config{}, fmt.Errorf("GRUS_RECONCILE_INTERVAL: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return config{}, err
	}
	if cfg.HostID == "" {
		cfg.HostID = defaultHostID(cfg.Driver, cfg.Namespace)
	}
	return cfg, nil
}

func (c config) validate() error {
	switch c.Driver {
	case "kubernetes":
		if c.Namespace == "" {
			return errors.New("GRUS_NAMESPACE is required for the kubernetes driver")
		}
	case "process":
	default:
		return fmt.Errorf("GRUS_DRIVER must be kubernetes or process, got %q", c.Driver)
	}
	if c.PublicURL == "" {
		return errors.New("GRUS_PUBLIC_URL is required")
	}
	if len(c.AdminToken) < minAdminTokenLen {
		return fmt.Errorf("GRUS_ADMIN_TOKEN must be at least %d characters", minAdminTokenLen)
	}
	if c.MaxInstances < 1 {
		return errors.New("GRUS_MAX_INSTANCES must be at least 1")
	}
	if c.ReconcileInterval <= 0 {
		return errors.New("GRUS_RECONCILE_INTERVAL must be positive")
	}
	return nil
}

func defaultHostID(driverName, namespace string) string {
	if driverName == "kubernetes" {
		return "k8s-" + namespace
	}
	return "local"
}

// splitList splits a comma separated list and drops empty entries.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseKeyValues parses "k=v,k2=v2". An empty string yields nil.
func parseKeyValues(env, s string) (map[string]string, error) {
	pairs := splitList(s)
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		k, v, ok := strings.Cut(pair, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("%s: entry %q is not k=v", env, pair)
		}
		out[k] = strings.TrimSpace(v)
	}
	return out, nil
}
