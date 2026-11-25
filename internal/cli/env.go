// Package cli implements the client side of the grus command: resource commands, contexts and the REPL.
package cli

import (
	"net/http"
	"os"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// completionTTL is how long completion candidates are reused before the API is asked again.
const completionTTL = 5 * time.Second

// Env carries everything the commands take from the outside world, so tests can replace it.
type Env struct {
	// Getenv reads environment variables; defaults to os.Getenv.
	Getenv func(string) string
	// ConfigDir returns the user config directory; defaults to os.UserConfigDir.
	ConfigDir func() (string, error)
	// HTTPClient is used for every API call; defaults to a client without a global timeout.
	HTTPClient *http.Client
	// Clock drives the completion cache; defaults to the system clock.
	Clock model.Clock
	// Exit ends the process (the shell's exit command); defaults to os.Exit.
	Exit func(int)

	cache *nameCache
}

// ensure fills the defaults. It is idempotent.
func (e *Env) ensure() {
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	if e.ConfigDir == nil {
		e.ConfigDir = os.UserConfigDir
	}
	if e.HTTPClient == nil {
		e.HTTPClient = &http.Client{}
	}
	if e.Clock == nil {
		e.Clock = model.SystemClock{}
	}
	if e.Exit == nil {
		e.Exit = os.Exit
	}
	if e.cache == nil {
		e.cache = newNameCache(e.Clock, completionTTL)
	}
}
