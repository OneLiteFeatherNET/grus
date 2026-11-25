package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
	"github.com/OneLiteFeatherNET/grus/internal/templates"
)

const (
	adminToken = "admin-secret-token"
	proxyToken = "proxy-instance-token"
	gameToken  = "game-instance-token"
)

// fakeService is a hand-written controller.Service. Each test sets only the fields it needs and
// inspects the recorded arguments afterwards.
type fakeService struct {
	instances map[string]model.Instance // by id
	tokens    map[string]string         // token -> instance id

	pool    model.Pool
	putErr  error
	putName string
	putSpec model.PoolSpec

	env        model.Environment
	envName    string
	envSpec    model.EnvironmentSpec
	deletedEnv string
	groupName  string
	network    model.NetworkStatus

	listFilter store.InstanceFilter
	listed     []model.Instance

	stopErr error
	stopID  string

	statusID string

	logsTail   int
	logsFollow bool
	logsBody   string
	logsErr    error
}

func newFakeService() *fakeService {
	return &fakeService{
		instances: map[string]model.Instance{
			"proxy-1": {ID: "proxy-1", Role: model.RoleProxy, Pool: "proxies", State: model.StateReady},
			"lobby-1": {ID: "lobby-1", Role: model.RoleGame, Pool: "lobby", State: model.StateReady},
		},
		tokens: map[string]string{proxyToken: "proxy-1", gameToken: "lobby-1"},
	}
}

func (f *fakeService) PutPool(_ context.Context, name string, spec model.PoolSpec) (model.Pool, error) {
	f.putName, f.putSpec = name, spec
	if f.putErr != nil {
		return model.Pool{}, f.putErr
	}
	return model.Pool{Name: name, Spec: spec, Generation: 1}, nil
}

func (f *fakeService) GetPool(context.Context, string) (model.Pool, error) { return f.pool, nil }

func (f *fakeService) ListPools(context.Context) ([]model.Pool, error) { return nil, nil }

func (f *fakeService) DeletePool(context.Context, string, bool) error { return nil }

func (f *fakeService) SetMaintenance(_ context.Context, name string, enabled bool) (model.Pool, error) {
	return model.Pool{Name: name, Maintenance: enabled}, nil
}

func (f *fakeService) PutEnvironment(_ context.Context, name string, spec model.EnvironmentSpec) (model.Environment, error) {
	f.envName, f.envSpec = name, spec
	if f.putErr != nil {
		return model.Environment{}, f.putErr
	}
	return model.Environment{Name: name, Spec: spec, Source: model.SourceAPI, Generation: 1}, nil
}

func (f *fakeService) GetEnvironment(context.Context, string) (model.Environment, error) {
	return f.env, nil
}

func (f *fakeService) ListEnvironments(context.Context) ([]model.Environment, error) { return nil, nil }

func (f *fakeService) DeleteEnvironment(_ context.Context, name string) error {
	f.deletedEnv = name
	return f.putErr
}

func (f *fakeService) PutGroup(_ context.Context, name string, spec model.GroupSpec) (model.Group, error) {
	f.groupName = name
	if f.putErr != nil {
		return model.Group{}, f.putErr
	}
	return model.Group{Name: name, Spec: spec, Source: model.SourceAPI, Generation: 1}, nil
}

func (f *fakeService) GetGroup(context.Context, string) (model.Group, error) {
	return model.Group{}, nil
}

func (f *fakeService) ListGroups(context.Context) ([]model.Group, error) { return nil, nil }

func (f *fakeService) DeleteGroup(context.Context, string) error { return f.putErr }

func (f *fakeService) Network(context.Context) (model.NetworkStatus, error) { return f.network, nil }

func (f *fakeService) ListInstances(_ context.Context, filter store.InstanceFilter) ([]model.Instance, error) {
	f.listFilter = filter
	return f.listed, nil
}

func (f *fakeService) GetInstance(_ context.Context, id string) (model.Instance, error) {
	inst, ok := f.instances[id]
	if !ok {
		return model.Instance{}, controller.ErrNotFound
	}
	return inst, nil
}

func (f *fakeService) CreateInstance(context.Context, string) (model.Instance, error) {
	return model.Instance{}, nil
}

func (f *fakeService) StopInstance(_ context.Context, id string, _ controller.StopOptions) (model.Instance, error) {
	f.stopID = id
	if f.stopErr != nil {
		return model.Instance{}, f.stopErr
	}
	return f.instances[id], nil
}

func (f *fakeService) Logs(_ context.Context, _ string, tail int, follow bool) (io.ReadCloser, error) {
	f.logsTail, f.logsFollow = tail, follow
	if f.logsErr != nil {
		return nil, f.logsErr
	}
	return io.NopCloser(strings.NewReader(f.logsBody)), nil
}

func (f *fakeService) Command(context.Context, string, string) error { return nil }

func (f *fakeService) AuthenticateInstance(_ context.Context, token string) (model.Instance, error) {
	id, ok := f.tokens[token]
	if !ok {
		return model.Instance{}, controller.ErrNotFound
	}
	return f.instances[id], nil
}

func (f *fakeService) SelfReady(_ context.Context, id string) (model.Instance, error) {
	return f.instances[id], nil
}

func (f *fakeService) SelfStatus(_ context.Context, id string, _ controller.StatusUpdate) (model.Instance, error) {
	f.statusID = id
	return f.instances[id], nil
}

func (f *fakeService) SelfAllocate(_ context.Context, id string) (model.Instance, error) {
	return f.instances[id], nil
}

func (f *fakeService) SelfShutdown(_ context.Context, id string) (model.Instance, error) {
	return f.instances[id], nil
}

// fakeTemplates is an in-memory templates.Store. A version's digest is "sha256:" plus the hex SHA-256
// of its archive bytes, which is enough for the API tests.
type fakeTemplates struct {
	archives map[string][]byte            // digest -> tar.gz bytes
	tags     map[string]map[string]string // name -> tag -> digest
}

func newFakeTemplates() *fakeTemplates {
	return &fakeTemplates{archives: map[string][]byte{}, tags: map[string]map[string]string{}}
}

// addArchive stores a gzip stream of content under its digest and returns that digest.
func (f *fakeTemplates) addArchive(t *testing.T, content string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	digest := "sha256:" + hex.EncodeToString(sum[:])
	f.archives[digest] = buf.Bytes()
	return digest
}

func (f *fakeTemplates) ImportTarGz(_ context.Context, name string, r io.Reader) (templates.Version, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return templates.Version{}, err
	}
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	f.archives[digest] = data
	return templates.Version{Name: name, Digest: digest, Size: int64(len(data))}, nil
}

func (f *fakeTemplates) ImportDir(context.Context, string, string) (templates.Version, error) {
	return templates.Version{}, errors.New("not used in api tests")
}

func (f *fakeTemplates) Tag(_ context.Context, name, tag, digest string) error {
	if _, ok := f.archives[digest]; !ok {
		return templates.ErrNotFound
	}
	if f.tags[name] == nil {
		f.tags[name] = map[string]string{}
	}
	f.tags[name][tag] = digest
	return nil
}

func (f *fakeTemplates) Resolve(_ context.Context, _, tagOrDigest string) (string, error) {
	if _, ok := f.archives[tagOrDigest]; !ok {
		return "", templates.ErrNotFound
	}
	return tagOrDigest, nil
}

func (f *fakeTemplates) List(context.Context) (map[string]map[string]string, error) {
	return f.tags, nil
}

func (f *fakeTemplates) WriteTarGz(_ context.Context, _, digest string, w io.Writer) error {
	data, ok := f.archives[digest]
	if !ok {
		return templates.ErrNotFound
	}
	_, err := w.Write(data)
	return err
}

// fixture bundles one handler with the fakes behind it. Every test builds its own.
type fixture struct {
	svc     *fakeService
	tpl     *fakeTemplates
	handler http.Handler
}

func newFixture(t *testing.T, cfg Config, events http.Handler) fixture {
	t.Helper()
	if cfg.AdminToken == "" {
		cfg.AdminToken = adminToken
	}
	svc := newFakeService()
	tpl := newFakeTemplates()
	return fixture{svc: svc, tpl: tpl, handler: New(cfg, svc, tpl, events, nil)}
}

// call sends one request through the handler. An empty token sends no Authorization header.
func (fx fixture) call(t *testing.T, method, target, token, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	fx.handler.ServeHTTP(rec, req)
	return rec
}

// gunzipOrFail returns the uncompressed content of a gzip body.
func gunzipOrFail(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("response is not gzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	return string(out)
}
