package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/driver/fake"
	"github.com/OneLiteFeatherNET/grus/internal/driver/router"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

const hashA = "a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3"

func TestParseAgentTokensReadsHostHashPairs(t *testing.T) {
	got, err := parseAgentTokens(" mc01=" + strings.ToUpper(hashA) + ", mc02=" + hashA + " ")
	if err != nil {
		t.Fatalf("parseAgentTokens: %v", err)
	}
	if len(got) != 2 || got["mc01"] != hashA || got["mc02"] != hashA {
		t.Errorf("tokens = %v, want two hosts with the lower case hash", got)
	}
	if none, err := parseAgentTokens(""); err != nil || none != nil {
		t.Errorf("empty list = %v, %v, want nil, nil", none, err)
	}
}

func TestParseAgentTokensRejectsMalformedEntriesWithoutEchoingTheValue(t *testing.T) {
	for _, in := range []string{"mc01", "=" + hashA, "mc01=not-hex", "mc01=abcd", "a/b=" + hashA} {
		_, err := parseAgentTokens(in)
		if err == nil {
			t.Errorf("parseAgentTokens(%q) returned nil error", in)
			continue
		}
		if strings.Contains(err.Error(), "not-hex") {
			t.Errorf("error %q echoes the token value", err)
		}
	}
}

func agentSpec(driverName, host string) driver.StartSpec {
	return driver.StartSpec{
		Pool:      model.Pool{Name: "p"},
		Effective: model.EffectiveSpec{Driver: driverName, Host: host, Command: []string{"x"}},
	}
}

func TestPlacementSelectorRoutesByEffectivePlacement(t *testing.T) {
	base := fake.New("k8s-grus-dev")
	online := map[string]bool{"mc01": true}
	sel := placementSelector(base, func(h string) bool { return online[h] })

	if host, err := sel(agentSpec("agent", "mc01")); err != nil || host != "mc01" {
		t.Errorf("agent on an online host = %q, %v, want mc01", host, err)
	}
	if host, err := sel(agentSpec("fake", "")); err != nil || host != "k8s-grus-dev" {
		t.Errorf("pool of the base driver = %q, %v, want the base host", host, err)
	}
	for name, spec := range map[string]driver.StartSpec{
		"offline agent":      agentSpec("agent", "mc02"),
		"agent without host": agentSpec("agent", ""),
		"foreign driver":     agentSpec("kubernetes", ""),
	} {
		if _, err := sel(spec); !errors.Is(err, router.ErrNoHost) {
			t.Errorf("%s: err = %v, want router.ErrNoHost", name, err)
		}
	}
}

func TestWithAgentsKnowsConfiguredHostsBeforeTheyConnect(t *testing.T) {
	base := fake.New("local")
	drv, reg, err := withAgents(base, config{AgentTokens: map[string]string{"mc01": hashA}})
	if err != nil {
		t.Fatalf("withAgents: %v", err)
	}
	_, err = drv.List(t.Context())
	var partial *router.PartialListError
	if !errors.As(err, &partial) || len(partial.Hosts) != 1 || partial.Hosts[0] != "mc01" {
		t.Errorf("List err = %v, want mc01 unlisted while its agent is away", err)
	}
	if reg.Online("mc01") {
		t.Error("mc01 online before any contact")
	}
}
