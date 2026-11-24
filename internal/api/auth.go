package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

// Scopes. Operations declare one of them; principals carry a set of them.
const (
	scopeSelf           = "self"
	scopeAdmin          = "admin" // debug endpoints only; the admin token has it, instances never do
	scopePoolsRead      = "pools:read"
	scopePoolsWrite     = "pools:write"
	scopeInstancesRead  = "instances:read"
	scopeInstancesWrite = "instances:write"
	scopeInstancesCons  = "instances:console"
	scopePlayersRead    = "players:read"
	scopePlayersConnect = "players:connect"
	scopePresenceWrite  = "presence:write"
	scopeEventsRead     = "events:read"
	scopeTemplatesRead  = "templates:read"
	scopeTemplatesWrite = "templates:write"
)

// metaScope is the huma Operation.Metadata key holding the scope an operation requires.
const metaScope = "grus.scope"

// principal is the authenticated caller of one request.
type principal struct {
	admin      bool
	instanceID string
	scopes     map[string]bool
}

// has reports whether the principal may call an operation that requires scope.
// The admin token has every scope except self: it has no instance identity.
func (p principal) has(scope string) bool {
	if p.admin {
		return scope != scopeSelf
	}
	return p.scopes[scope]
}

var adminPrincipal = principal{admin: true}

// instancePrincipal derives the scopes of an instance token from its role.
func instancePrincipal(inst model.Instance) principal {
	scopes := map[string]bool{
		scopeSelf:           true,
		scopeInstancesRead:  true,
		scopePlayersConnect: true,
		scopeTemplatesRead:  true,
	}
	if inst.Role == model.RoleProxy {
		scopes[scopeEventsRead] = true
		scopes[scopePlayersRead] = true
		scopes[scopePresenceWrite] = true
	}
	return principal{instanceID: inst.ID, scopes: scopes}
}

type principalKey struct{}

func withPrincipal(ctx context.Context, p principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// principalFrom returns the principal authenticated for the request. Handlers only run after the
// middleware or raw wrapper has put one in place.
func principalFrom(ctx context.Context) principal {
	p, _ := ctx.Value(principalKey{}).(principal)
	return p
}

var errUnauthorized = errors.New("missing or invalid bearer token")

type authenticator struct {
	// adminDigest is the SHA-256 of the admin token; hashing first makes the comparison length independent.
	adminDigest [sha256.Size]byte
	adminSet    bool
	svc         controller.Service
	log         *slog.Logger
}

func newAuthenticator(adminToken string, svc controller.Service, logger *slog.Logger) *authenticator {
	a := &authenticator{svc: svc, log: logger}
	if adminToken != "" {
		a.adminDigest = sha256.Sum256([]byte(adminToken))
		a.adminSet = true
	}
	return a
}

// authenticate maps an Authorization header to a principal. It never logs the token.
func (a *authenticator) authenticate(ctx context.Context, header string) (principal, error) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return principal{}, errUnauthorized
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return principal{}, errUnauthorized
	}
	if a.adminSet {
		digest := sha256.Sum256([]byte(token))
		if subtle.ConstantTimeCompare(digest[:], a.adminDigest[:]) == 1 {
			return adminPrincipal, nil
		}
	}
	inst, err := a.svc.AuthenticateInstance(ctx, token)
	if err != nil {
		if !errors.Is(err, controller.ErrNotFound) && !errors.Is(err, store.ErrNotFound) {
			a.log.Error("api: instance token lookup failed", "err", err)
		}
		return principal{}, errUnauthorized
	}
	return instancePrincipal(inst), nil
}

// middleware authenticates every huma operation and checks the operation's scope.
func (a *authenticator) middleware(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		p, err := a.authenticate(ctx.Context(), ctx.Header("Authorization"))
		if err != nil {
			ctx.SetHeader("WWW-Authenticate", "Bearer")
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, err.Error())
			return
		}
		scope, _ := ctx.Operation().Metadata[metaScope].(string)
		if !p.has(scope) {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "token lacks scope "+scope)
			return
		}
		next(huma.WithContext(ctx, withPrincipal(ctx.Context(), p)))
	}
}

// raw wraps a plain net/http handler with the same authentication and scope check.
func (a *authenticator) raw(scope string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.authenticate(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeProblem(w, huma.Error401Unauthorized(err.Error()))
			return
		}
		if !p.has(scope) {
			writeProblem(w, huma.Error403Forbidden("token lacks scope "+scope))
			return
		}
		h.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), p)))
	})
}
