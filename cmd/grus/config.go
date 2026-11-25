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
	TemplatesBackend     string
	S3Endpoint           string
	S3Bucket             string
	S3Prefix             string
	S3Insecure           bool
	S3AccessKey          string
	S3SecretKey          string
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
	AgentTokens          map[string]string // host -> hex SHA-256 of the agent token
	ProcessDir           string
	ReconcileInterval    time.Duration
	CRDs                 bool
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
	templatesBackend := str("templates-backend", "GRUS_TEMPLATES_BACKEND", "fs", "template store backend: fs or s3")
	s3Endpoint := str("s3-endpoint", "GRUS_S3_ENDPOINT", "", "S3 endpoint for the s3 template backend: host:port or http(s) URL")
	s3Bucket := str("s3-bucket", "GRUS_S3_BUCKET", "", "S3 bucket of the s3 template backend")
	s3Prefix := str("s3-prefix", "GRUS_S3_PREFIX", "", "key prefix inside the bucket, optional")
	s3Insecure := str("s3-insecure", "GRUS_S3_INSECURE", "false", "use plain HTTP for a host:port S3 endpoint (true/false)")
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
	crds := str("crds", "GRUS_CRDS", "false", "watch the Grus custom resources of the namespace (true/false)")
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
		TemplatesBackend:     *templatesBackend,
		S3Endpoint:           *s3Endpoint,
		S3Bucket:             *s3Bucket,
		S3Prefix:             *s3Prefix,
		S3AccessKey:          getenv("AWS_ACCESS_KEY_ID"),
		S3SecretKey:          getenv("AWS_SECRET_ACCESS_KEY"),
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
	if cfg.AgentTokens, err = parseAgentTokens(getenv("GRUS_AGENT_TOKENS")); err != nil {
		return config{}, err
	}
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
	if cfg.CRDs, err = strconv.ParseBool(*crds); err != nil {
		return config{}, fmt.Errorf("GRUS_CRDS: %w", err)
	}
	if cfg.S3Insecure, err = strconv.ParseBool(*s3Insecure); err != nil {
		return config{}, fmt.Errorf("GRUS_S3_INSECURE: %w", err)
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
	if c.CRDs && c.Namespace == "" {
		return errors.New("GRUS_NAMESPACE is required for GRUS_CRDS")
	}
	switch c.TemplatesBackend {
	case "fs":
	case "s3":
		if err := c.validateS3(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("GRUS_TEMPLATES_BACKEND must be fs or s3, got %q", c.TemplatesBackend)
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

// validateS3 checks that the s3 template backend has everything it needs. Credentials are checked for
// presence only: their values never appear in an error.
func (c config) validateS3() error {
	if c.S3Endpoint == "" {
		return errors.New("GRUS_S3_ENDPOINT is required for the s3 template backend")
	}
	if c.S3Bucket == "" {
		return errors.New("GRUS_S3_BUCKET is required for the s3 template backend")
	}
	if c.S3AccessKey == "" {
		return errors.New("AWS_ACCESS_KEY_ID is required for the s3 template backend")
	}
	if c.S3SecretKey == "" {
		return errors.New("AWS_SECRET_ACCESS_KEY is required for the s3 template backend")
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
