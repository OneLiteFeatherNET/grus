package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/driver/remote"
	"github.com/OneLiteFeatherNET/grus/internal/driver/router"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// parseAgentTokens parses GRUS_AGENT_TOKENS: a comma list of host=<sha256 hex of the agent token>.
// Only hashes are configured, so the setting is safe to keep in a ConfigMap-like place.
func parseAgentTokens(s string) (map[string]string, error) {
	pairs := splitList(s)
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		host, hash, ok := strings.Cut(pair, "=")
		host, hash = strings.TrimSpace(host), strings.ToLower(strings.TrimSpace(hash))
		if !ok || host == "" {
			return nil, errors.New("GRUS_AGENT_TOKENS: every entry must be host=<sha256 hex of the token>")
		}
		if strings.Contains(host, "/") {
			return nil, fmt.Errorf("GRUS_AGENT_TOKENS: host %q must not contain /", host)
		}
		if raw, err := hex.DecodeString(hash); err != nil || len(raw) != sha256.Size {
			return nil, fmt.Errorf("GRUS_AGENT_TOKENS: the value for host %q is not a SHA-256 hex digest", host)
		}
		out[host] = hash
	}
	return out, nil
}

// withAgents wraps the configured driver and the agent hosts in a router. The configured driver stays one
// host of its own; every host named in the agent tokens is registered up front, so its instances keep their
// state while the agent is away, and further hosts join when an agent contacts the controller.
func withAgents(base driver.Driver, cfg config) (driver.Driver, *remote.Registry, error) {
	var reg *remote.Registry
	rtr := router.New(placementSelector(base, func(host string) bool { return reg != nil && reg.Online(host) }))
	reg = remote.NewRegistry(rtr, remote.Config{})
	if err := rtr.Add(base); err != nil {
		return nil, nil, fmt.Errorf("add driver host: %w", err)
	}
	for host := range cfg.AgentTokens {
		if err := reg.Preregister(host); err != nil {
			return nil, nil, fmt.Errorf("add agent host %s: %w", host, err)
		}
	}
	return rtr, reg, nil
}

// placementSelector picks the host by the effective placement: kubernetes and process go to the configured
// driver when it is that kind, agent goes to the host named in the placement and must be online.
func placementSelector(base driver.Driver, online func(host string) bool) router.Selector {
	return func(spec driver.StartSpec) (string, error) {
		eff := spec.EffectiveOrV1()
		switch eff.Driver {
		case model.DriverAgent:
			if eff.Host == "" {
				return "", fmt.Errorf("%w: pool %s has no placement.host", router.ErrNoHost, spec.Pool.Name)
			}
			if !online(eff.Host) {
				return "", fmt.Errorf("%w: agent host %q is not registered or offline", router.ErrNoHost, eff.Host)
			}
			return eff.Host, nil
		case "", base.Name():
			return base.Host(), nil
		default:
			return "", fmt.Errorf("%w: this controller runs the %s driver, pool %s is placed on %s",
				router.ErrNoHost, base.Name(), spec.Pool.Name, eff.Driver)
		}
	}
}
