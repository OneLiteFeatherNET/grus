package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/OneLiteFeatherNET/grus/internal/agent"
	"os"
	"strconv"
	"strings"
)

// parseConfig parses args on top of the environment read through getenv. hostname supplies the default host.
func parseConfig(args []string, getenv func(string) string, hostname func() (string, error)) (agent.Config, error) {
	fs := flag.NewFlagSet("grus-agent", flag.ContinueOnError)
	str := func(name, env, def, usage string) *string {
		v := getenv(env)
		if v == "" {
			v = def
		}
		return fs.String(name, v, usage+" ["+env+"]")
	}
	defHost := ""
	if h, err := hostname(); err == nil {
		defHost = strings.ToLower(h)
	}
	url := str("url", "GRUS_URL", "", "controller URL, e.g. https://grus.example.org (required)")
	tokenFile := str("token-file", "GRUS_AGENT_TOKEN_FILE", "", "file holding the agent token (required)")
	host := str("host", "GRUS_AGENT_HOST", defHost, "host name this agent registers as")
	dir := str("dir", "GRUS_AGENT_DIR", "./instances", "working directory of the instances")
	address := str("address", "GRUS_AGENT_ADDRESS", "", "IP address proxies use to reach the instances (required)")
	portRange := str("port-range", "GRUS_AGENT_PORT_RANGE", "30000-30100", "instance port range min-max")
	java := str("java", "GRUS_AGENT_JAVA", "", "java binary used for commands that start with java")
	caFile := str("ca-file", "GRUS_AGENT_CA_FILE", "", "extra CA certificates (PEM) for the controller URL, optional")
	stopOnExit := str("stop-on-exit", "GRUS_AGENT_STOP_ON_EXIT", "false", "stop the instances when the agent exits (true/false)")
	if err := fs.Parse(args); err != nil {
		return agent.Config{}, err
	}
	if fs.NArg() > 0 {
		return agent.Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	cfg := agent.Config{Version: version, URL: strings.TrimSuffix(*url, "/"), Host: *host, Dir: *dir, Address: *address, Java: *java, CAFile: *caFile}
	switch {
	case cfg.URL == "":
		return agent.Config{}, errors.New("GRUS_URL (--url) is required")
	case !strings.HasPrefix(cfg.URL, "https://") && !strings.HasPrefix(cfg.URL, "http://"):
		return agent.Config{}, errors.New("GRUS_URL must start with https:// or http://")
	case cfg.Host == "" || strings.ContainsAny(cfg.Host, "/ "):
		return agent.Config{}, errors.New("--host must be a host name without slashes or spaces")
	case cfg.Address == "":
		return agent.Config{}, errors.New("GRUS_AGENT_ADDRESS (--address) is required: the IP proxies use to reach the instances")
	case *tokenFile == "":
		return agent.Config{}, errors.New("GRUS_AGENT_TOKEN_FILE (--token-file) is required")
	}
	raw, err := os.ReadFile(*tokenFile)
	if err != nil {
		return agent.Config{}, fmt.Errorf("read token file: %w", err)
	}
	if cfg.Token = strings.TrimSpace(string(raw)); cfg.Token == "" {
		return agent.Config{}, errors.New("token file is empty")
	}
	lo, hi, ok := strings.Cut(*portRange, "-")
	if cfg.PortMin, err = strconv.Atoi(lo); err != nil || !ok {
		return agent.Config{}, fmt.Errorf("--port-range %q must look like 30000-30100", *portRange)
	}
	if cfg.PortMax, err = strconv.Atoi(hi); err != nil || cfg.PortMin < 1 || cfg.PortMax < cfg.PortMin || cfg.PortMax > 65535 {
		return agent.Config{}, fmt.Errorf("--port-range %q must look like 30000-30100", *portRange)
	}
	if cfg.StopOnExit, err = strconv.ParseBool(*stopOnExit); err != nil {
		return agent.Config{}, fmt.Errorf("GRUS_AGENT_STOP_ON_EXIT: %w", err)
	}
	return cfg, nil
}
