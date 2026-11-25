package kubernetes

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

const testNamespace = "feather-core"

func newTestDriver(t *testing.T, objs ...runtime.Object) (*Driver, *fake.Clientset) {
	t.Helper()
	client := fake.NewClientset(objs...)
	cfg := Config{
		Namespace:            testNamespace,
		HostID:               "k8s-grus-dev",
		SelfImage:            "harbor.example/grus:1.2.3",
		ImagePullSecrets:     []string{"harbor-pull"},
		DefaultPriorityClass: "feather-default",
		NodeSelector:         map[string]string{"node-role": "game"},
		PodLabels:            map[string]string{"logs.onelitefeather.net/format": "json", "grus.olf/pool": "ignored-by-driver"},
	}
	return New(client, cfg), client
}

func testStartSpec(templates bool) driver.StartSpec {
	spec := driver.StartSpec{
		Instance: model.Instance{
			ID:             "lobby-7x2k",
			Pool:           "lobby",
			Role:           model.RoleGame,
			PoolGeneration: 4,
			TokenHash:      "sha256-of-token",
		},
		Pool: model.Pool{
			Name: "lobby",
			Spec: model.PoolSpec{
				Role:      model.RoleGame,
				Mode:      model.ModeDynamic,
				Driver:    "kubernetes",
				Image:     "registry.example/lobby:2.0",
				Command:   []string{"java"},
				Args:      []string{"-jar", "server.jar"},
				SecretEnv: []model.SecretRef{{Name: "DB_PASSWORD", Secret: "lobby-db", Key: "password"}},
				Resources: model.Resources{MemoryMiB: 512, CPU: "250m"},
				Port:      25566,
				Labels:    map[string]string{"team": "olf", driver.LabelRole: "user-role-must-lose"},
				Lifecycle: model.Lifecycle{GracePeriod: model.Duration(45 * time.Second)},
			},
		},
		Token: "plain-secret-token",
		Env: map[string]string{
			"GRUS_URL":      "https://grus.example",
			"GRUS_INSTANCE": "lobby-7x2k",
			"GRUS_POOL":     "lobby",
			"GRUS_ROLE":     "game",
			"POOL_HINT":     "spec-env",
			"ZETA":          "last",
			"ALPHA":         "first",
		},
	}
	if templates {
		spec.Templates = []driver.ResolvedTemplate{
			{Name: "base", Digest: "sha256:aaa"},
			{Name: "lobby-maps", Digest: "sha256:bbb"},
		}
	}
	return spec
}

func startPod(t *testing.T, d *Driver, spec driver.StartSpec) *corev1.Pod {
	t.Helper()
	ref, err := d.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	pod := getPod(t, d, ref)
	return pod
}

func getPod(t *testing.T, d *Driver, name string) *corev1.Pod {
	t.Helper()
	pod, err := d.client.CoreV1().Pods(testNamespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pod %s: %v", name, err)
	}
	return pod
}

func containerNamed(t *testing.T, containers []corev1.Container, name string) corev1.Container {
	t.Helper()
	for _, c := range containers {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("container %q not found in %v", name, containers)
	return corev1.Container{}
}

func envValue(env []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
	for _, e := range env {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

func TestStartNamesPodAfterInstanceAndUsesPoolNamespace(t *testing.T) {
	d, _ := newTestDriver(t)
	ref, err := d.Start(context.Background(), testStartSpec(false))
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ref != "lobby-7x2k" {
		t.Fatalf("ref = %q, want pod name %q", ref, "lobby-7x2k")
	}
	pod := getPod(t, d, ref)
	if pod.Namespace != testNamespace {
		t.Fatalf("pod namespace = %q, want %q", pod.Namespace, testNamespace)
	}
}

func TestStartSetsDriverLabelsAndWinsOverUserLabels(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	want := map[string]string{
		driver.LabelManagedBy:            "grus",
		driver.LabelPool:                 "lobby",
		driver.LabelInstance:             "lobby-7x2k",
		driver.LabelRole:                 "game",
		"logs.onelitefeather.net/format": "json",
		"team":                           "olf",
	}
	if !reflect.DeepEqual(pod.Labels, want) {
		t.Fatalf("pod labels = %v, want %v", pod.Labels, want)
	}
}

func TestStartSetsGrusAnnotations(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	want := map[string]string{
		driver.AnnotationGeneration:  "4",
		driver.AnnotationTokenSHA256: "sha256-of-token",
		driver.AnnotationMode:        "dynamic",
	}
	for k, v := range want {
		if got := pod.Annotations[k]; got != v {
			t.Errorf("annotation %s = %q, want %q", k, got, v)
		}
	}
}

func TestStartSetsPodSpecRestartAndSchedulingFields(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restartPolicy = %q, want Never", pod.Spec.RestartPolicy)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Errorf("automountServiceAccountToken must be explicitly false")
	}
	if pod.Spec.EnableServiceLinks == nil || *pod.Spec.EnableServiceLinks {
		t.Errorf("enableServiceLinks must be explicitly false")
	}
	if pod.Spec.TerminationGracePeriodSeconds == nil || *pod.Spec.TerminationGracePeriodSeconds != 45 {
		t.Errorf("terminationGracePeriodSeconds = %v, want 45", pod.Spec.TerminationGracePeriodSeconds)
	}
	if pod.Spec.PriorityClassName != "feather-default" {
		t.Errorf("priorityClassName = %q, want the driver default feather-default", pod.Spec.PriorityClassName)
	}
	if !reflect.DeepEqual(pod.Spec.NodeSelector, map[string]string{"node-role": "game"}) {
		t.Errorf("nodeSelector = %v, want node-role=game", pod.Spec.NodeSelector)
	}
	if len(pod.Spec.ImagePullSecrets) != 1 || pod.Spec.ImagePullSecrets[0].Name != "harbor-pull" {
		t.Errorf("imagePullSecrets = %v, want harbor-pull", pod.Spec.ImagePullSecrets)
	}
}

func TestStartPrefersPoolPriorityClassOverDefault(t *testing.T) {
	d, _ := newTestDriver(t)
	spec := testStartSpec(false)
	spec.Pool.Spec.PriorityClassName = "feather-low"
	pod := startPod(t, d, spec)

	if pod.Spec.PriorityClassName != "feather-low" {
		t.Fatalf("priorityClassName = %q, want pool value feather-low", pod.Spec.PriorityClassName)
	}
}

func TestStartAppliesRestrictedPodSecurityContext(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	sc := pod.Spec.SecurityContext
	if sc == nil {
		t.Fatal("pod securityContext is nil")
	}
	if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Error("runAsNonRoot must be true")
	}
	for name, got := range map[string]*int64{"runAsUser": sc.RunAsUser, "runAsGroup": sc.RunAsGroup, "fsGroup": sc.FSGroup} {
		if got == nil || *got != 10001 {
			t.Errorf("%s = %v, want 10001", name, got)
		}
	}
	if sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("seccompProfile = %v, want RuntimeDefault", sc.SeccompProfile)
	}
}

func TestStartAppliesRestrictedContainerSecurityContextToServer(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	server := containerNamed(t, pod.Spec.Containers, "server")
	assertRestrictedContainer(t, "server", server.SecurityContext)
}

func TestStartAppliesRestrictedContainerSecurityContextToFetch(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(true))

	fetch := containerNamed(t, pod.Spec.InitContainers, "fetch")
	assertRestrictedContainer(t, "fetch", fetch.SecurityContext)
}

func assertRestrictedContainer(t *testing.T, name string, sc *corev1.SecurityContext) {
	t.Helper()
	if sc == nil {
		t.Fatalf("%s: securityContext is nil", name)
	}
	if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		t.Errorf("%s: allowPrivilegeEscalation must be false", name)
	}
	if sc.Capabilities == nil || !reflect.DeepEqual(sc.Capabilities.Drop, []corev1.Capability{"ALL"}) {
		t.Errorf("%s: capabilities.drop = %v, want [ALL]", name, sc.Capabilities)
	}
	if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Errorf("%s: runAsNonRoot must be true", name)
	}
	if sc.ReadOnlyRootFilesystem == nil || *sc.ReadOnlyRootFilesystem {
		t.Errorf("%s: readOnlyRootFilesystem must be false", name)
	}
}

func TestStartSetsServerContainerImageCommandAndWorkingDir(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	server := containerNamed(t, pod.Spec.Containers, "server")
	if server.Image != "registry.example/lobby:2.0" {
		t.Errorf("image = %q", server.Image)
	}
	if !reflect.DeepEqual(server.Command, []string{"java"}) || !reflect.DeepEqual(server.Args, []string{"-jar", "server.jar"}) {
		t.Errorf("command/args = %v %v, want [java] [-jar server.jar]", server.Command, server.Args)
	}
	if server.WorkingDir != "/data" {
		t.Errorf("workingDir = %q, want /data", server.WorkingDir)
	}
	if !server.Stdin || server.TTY {
		t.Errorf("stdin = %v tty = %v, want stdin true and tty false", server.Stdin, server.TTY)
	}
}

func TestStartExposesMinecraftPortOnServerContainer(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	server := containerNamed(t, pod.Spec.Containers, "server")
	want := []corev1.ContainerPort{{Name: "minecraft", ContainerPort: 25566, Protocol: corev1.ProtocolTCP}}
	if !reflect.DeepEqual(server.Ports, want) {
		t.Fatalf("ports = %v, want %v", server.Ports, want)
	}
}

func TestStartSetsMemoryRequestAndLimitEqualAndCPURequestOnly(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	server := containerNamed(t, pod.Spec.Containers, "server")
	mem := resource.MustParse("512Mi")
	if got := server.Resources.Requests.Memory(); got.Cmp(mem) != 0 {
		t.Errorf("memory request = %s, want 512Mi", got.String())
	}
	if got := server.Resources.Limits.Memory(); got.Cmp(mem) != 0 {
		t.Errorf("memory limit = %s, want 512Mi", got.String())
	}
	if got := server.Resources.Requests.Cpu(); got.Cmp(resource.MustParse("250m")) != 0 {
		t.Errorf("cpu request = %s, want 250m", got.String())
	}
	if _, ok := server.Resources.Limits[corev1.ResourceCPU]; ok {
		t.Errorf("cpu limit must not be set, got %s", server.Resources.Limits.Cpu().String())
	}
}

func TestStartPassesSpecEnvSortedThenTokenThenSecretEnv(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	server := containerNamed(t, pod.Spec.Containers, "server")
	names := make([]string, 0, len(server.Env))
	for _, e := range server.Env {
		names = append(names, e.Name)
	}
	want := []string{"ALPHA", "GRUS_INSTANCE", "GRUS_POOL", "GRUS_ROLE", "GRUS_URL", "POOL_HINT", "ZETA", "GRUS_TOKEN", "DB_PASSWORD"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("env names = %v, want %v", names, want)
	}
	if alpha, _ := envValue(server.Env, "ALPHA"); alpha.Value != "first" {
		t.Errorf("ALPHA = %q, want first", alpha.Value)
	}
}

func TestStartInjectsTokenFromSecretKeyRef(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	server := containerNamed(t, pod.Spec.Containers, "server")
	token, ok := envValue(server.Env, "GRUS_TOKEN")
	if !ok {
		t.Fatal("GRUS_TOKEN env missing")
	}
	if token.Value != "" {
		t.Errorf("GRUS_TOKEN must not be a plain value, got %q", token.Value)
	}
	ref := token.ValueFrom.SecretKeyRef
	if ref == nil || ref.Name != "grus-token-lobby-7x2k" || ref.Key != "token" {
		t.Fatalf("GRUS_TOKEN secretKeyRef = %+v, want grus-token-lobby-7x2k/token", ref)
	}
}

func TestStartInjectsPoolSecretEnvFromSecretKeyRef(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	server := containerNamed(t, pod.Spec.Containers, "server")
	db, ok := envValue(server.Env, "DB_PASSWORD")
	if !ok {
		t.Fatal("DB_PASSWORD env missing")
	}
	ref := db.ValueFrom.SecretKeyRef
	if ref == nil || ref.Name != "lobby-db" || ref.Key != "password" {
		t.Fatalf("DB_PASSWORD secretKeyRef = %+v, want lobby-db/password", ref)
	}
}

func TestStartMountsEmptyDataVolumeIntoEveryContainer(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(true))

	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].Name != "data" || pod.Spec.Volumes[0].EmptyDir == nil {
		t.Fatalf("volumes = %+v, want one emptyDir named data", pod.Spec.Volumes)
	}
	for _, c := range append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...) {
		if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].Name != "data" || c.VolumeMounts[0].MountPath != "/data" {
			t.Errorf("container %s mounts = %+v, want data at /data", c.Name, c.VolumeMounts)
		}
	}
}

func TestStartWithoutTemplatesAddsNoInitContainer(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	if len(pod.Spec.InitContainers) != 0 {
		t.Fatalf("init containers = %+v, want none", pod.Spec.InitContainers)
	}
}

func TestStartWithTemplatesAddsFetchInitContainer(t *testing.T) {
	d, _ := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(true))

	if len(pod.Spec.InitContainers) != 1 {
		t.Fatalf("init containers = %d, want 1", len(pod.Spec.InitContainers))
	}
	fetch := containerNamed(t, pod.Spec.InitContainers, "fetch")
	if fetch.Image != "harbor.example/grus:1.2.3" {
		t.Errorf("fetch image = %q, want the grus self image", fetch.Image)
	}
	if !reflect.DeepEqual(fetch.Command, []string{"/grus", "fetch"}) {
		t.Errorf("fetch command = %v, want [/grus fetch]", fetch.Command)
	}
	if !reflect.DeepEqual(fetch.Args, []string{"--dest", "/data"}) {
		t.Errorf("fetch args = %v, want [--dest /data]", fetch.Args)
	}
	if e, _ := envValue(fetch.Env, "GRUS_URL"); e.Value != "https://grus.example" {
		t.Errorf("fetch GRUS_URL = %q, want https://grus.example", e.Value)
	}
	if e, _ := envValue(fetch.Env, "GRUS_FETCH_TEMPLATES"); e.Value != "base@sha256:aaa,lobby-maps@sha256:bbb" {
		t.Errorf("fetch GRUS_FETCH_TEMPLATES = %q, want base@sha256:aaa,lobby-maps@sha256:bbb", e.Value)
	}
	token, _ := envValue(fetch.Env, "GRUS_TOKEN")
	if token.ValueFrom == nil || token.ValueFrom.SecretKeyRef == nil || token.ValueFrom.SecretKeyRef.Name != "grus-token-lobby-7x2k" {
		t.Errorf("fetch GRUS_TOKEN = %+v, want secretKeyRef to grus-token-lobby-7x2k", token)
	}
	wantRes := map[corev1.ResourceName]string{corev1.ResourceMemory: "32Mi", corev1.ResourceCPU: "10m"}
	for name, q := range wantRes {
		got := fetch.Resources.Requests[name]
		if got.Cmp(resource.MustParse(q)) != 0 {
			t.Errorf("fetch request %s = %s, want %s", name, got.String(), q)
		}
	}
	if got := fetch.Resources.Limits.Memory(); got.Cmp(resource.MustParse("64Mi")) != 0 {
		t.Errorf("fetch memory limit = %s, want 64Mi", got.String())
	}
}

func TestStartCreatesTokenSecretOwnedByPod(t *testing.T) {
	d, client := newTestDriver(t)
	pod := startPod(t, d, testStartSpec(false))

	secret, err := client.CoreV1().Secrets(testNamespace).Get(context.Background(), "grus-token-lobby-7x2k", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("token secret missing: %v", err)
	}
	if secret.Type != corev1.SecretTypeOpaque {
		t.Errorf("secret type = %q, want Opaque", secret.Type)
	}
	if string(secret.Data["token"]) != "plain-secret-token" {
		t.Errorf("secret token value not stored under key token")
	}
	if secret.Labels[driver.LabelManagedBy] != "grus" || secret.Labels[driver.LabelInstance] != "lobby-7x2k" {
		t.Errorf("secret labels = %v, want grus labels", secret.Labels)
	}
	owners := secret.OwnerReferences
	if len(owners) != 1 {
		t.Fatalf("owner references = %+v, want exactly one", owners)
	}
	owner := owners[0]
	if owner.APIVersion != "v1" || owner.Kind != "Pod" || owner.Name != pod.Name || owner.UID != pod.UID {
		t.Errorf("owner reference = %+v, want v1 Pod %s", owner, pod.Name)
	}
	if owner.Controller == nil || !*owner.Controller {
		t.Errorf("owner reference must be a controller reference")
	}
}

func TestStartDeletesPodWhenTokenSecretCreationFails(t *testing.T) {
	d, client := newTestDriver(t)
	client.PrependReactor("create", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("quota exceeded for secrets")
	})

	_, err := d.Start(context.Background(), testStartSpec(false))
	if err == nil {
		t.Fatal("Start returned nil error although the token secret could not be created")
	}
	_, getErr := client.CoreV1().Pods(testNamespace).Get(context.Background(), "lobby-7x2k", metav1.GetOptions{})
	if !apierrors.IsNotFound(getErr) {
		t.Fatalf("pod still exists after failed secret creation (get err: %v)", getErr)
	}
}

func TestListMapsRunningPodWithIPToAddress(t *testing.T) {
	pod := managedPod("lobby-1", "lobby", "game", "dynamic", 3)
	pod.Status = corev1.PodStatus{
		Phase: corev1.PodRunning,
		PodIP: "10.0.0.5",
		ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "server",
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}},
	}
	obs := listOne(t, pod)

	if obs.Phase != driver.PhaseRunning {
		t.Errorf("phase = %s, want Running", obs.Phase)
	}
	if obs.Address != "10.0.0.5:25565" {
		t.Errorf("address = %q, want 10.0.0.5:25565", obs.Address)
	}
}

func TestListMapsImagePullBackOffToFailed(t *testing.T) {
	pod := managedPod("lobby-2", "lobby", "game", "dynamic", 3)
	pod.Status = corev1.PodStatus{
		Phase: corev1.PodPending,
		ContainerStatuses: []corev1.ContainerStatus{{
			Name: "server",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
				Reason:  "ImagePullBackOff",
				Message: "back-off pulling image",
			}},
		}},
	}
	obs := listOne(t, pod)

	if obs.Phase != driver.PhaseFailed {
		t.Fatalf("phase = %s, want Failed", obs.Phase)
	}
	if obs.Reason != "ImagePullBackOff: back-off pulling image" {
		t.Errorf("reason = %q, want the waiting reason and message", obs.Reason)
	}
}

func TestListKeepsPendingForCreateContainerConfigError(t *testing.T) {
	pod := managedPod("lobby-3", "lobby", "game", "dynamic", 3)
	pod.Status = corev1.PodStatus{
		Phase: corev1.PodPending,
		ContainerStatuses: []corev1.ContainerStatus{{
			Name: "server",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
				Reason: "CreateContainerConfigError",
			}},
		}},
	}
	obs := listOne(t, pod)

	if obs.Phase != driver.PhasePending {
		t.Fatalf("phase = %s, want Pending", obs.Phase)
	}
	if obs.Reason != "CreateContainerConfigError" {
		t.Errorf("reason = %q, want CreateContainerConfigError", obs.Reason)
	}
}

func TestListMapsFailedInitContainerToFailedWithTemplateReason(t *testing.T) {
	pod := managedPod("lobby-4", "lobby", "game", "dynamic", 3)
	pod.Status = corev1.PodStatus{
		Phase: corev1.PodFailed,
		InitContainerStatuses: []corev1.ContainerStatus{{
			Name: "fetch",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 1,
				Reason:   "Error",
			}},
		}},
	}
	obs := listOne(t, pod)

	if obs.Phase != driver.PhaseFailed {
		t.Fatalf("phase = %s, want Failed", obs.Phase)
	}
	if obs.Reason != "template fetch failed: Error (exit code 1)" {
		t.Errorf("reason = %q, want template fetch failed with exit code", obs.Reason)
	}
}

func TestListMapsSucceededPodToExited(t *testing.T) {
	pod := managedPod("lobby-5", "lobby", "game", "dynamic", 3)
	pod.Status = corev1.PodStatus{
		Phase: corev1.PodSucceeded,
		ContainerStatuses: []corev1.ContainerStatus{{
			Name: "server",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 0,
				Reason:   "Completed",
			}},
		}},
	}
	obs := listOne(t, pod)

	if obs.Phase != driver.PhaseExited {
		t.Fatalf("phase = %s, want Exited", obs.Phase)
	}
	if obs.Reason != "Completed (exit code 0)" {
		t.Errorf("reason = %q, want the terminated reason and exit code", obs.Reason)
	}
}

func TestListMarksDeletingPodAsTerminating(t *testing.T) {
	pod := managedPod("lobby-6", "lobby", "game", "dynamic", 3)
	now := metav1.NewTime(time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	pod.DeletionTimestamp = &now
	pod.Status = corev1.PodStatus{
		Phase: corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "server",
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}},
	}
	obs := listOne(t, pod)

	if obs.Phase != driver.PhaseRunning {
		t.Errorf("phase = %s, want Running while terminating", obs.Phase)
	}
	if obs.Reason != "terminating" {
		t.Errorf("reason = %q, want terminating", obs.Reason)
	}
}

func TestListIgnoresPodsNotManagedByGrus(t *testing.T) {
	managed := managedPod("lobby-7", "lobby", "game", "dynamic", 3)
	unmanaged := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      "someone-else",
		Namespace: testNamespace,
		Labels:    map[string]string{"app": "other"},
	}}
	d, _ := newTestDriver(t, managed, unmanaged)

	obs, err := d.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(obs) != 1 || obs[0].Ref != "lobby-7" {
		t.Fatalf("List = %+v, want only the grus pod lobby-7", obs)
	}
}

func TestListMapsIdentityFromLabelsAndAnnotations(t *testing.T) {
	pod := managedPod("lobby-8", "lobby", "game", "static", 9)
	obs := listOne(t, pod)

	if obs.InstanceID != "lobby-8" || obs.Pool != "lobby" || obs.Role != model.RoleGame {
		t.Errorf("identity = instance %q pool %q role %q", obs.InstanceID, obs.Pool, obs.Role)
	}
	if obs.Mode != model.ModeStatic || obs.PoolGeneration != 9 || obs.TokenHash != "hash-lobby-8" {
		t.Errorf("mode/generation/tokenHash = %q/%d/%q", obs.Mode, obs.PoolGeneration, obs.TokenHash)
	}
}

func TestStopDeletesPodWithGracePeriod(t *testing.T) {
	pod := managedPod("lobby-9", "lobby", "game", "dynamic", 1)
	d, client := newTestDriver(t, pod)

	var deleteOpts metav1.DeleteOptions
	client.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleteOpts = action.(k8stesting.DeleteActionImpl).DeleteOptions
		return false, nil, nil
	})

	if err := d.Stop(context.Background(), "lobby-9", "", 30*time.Second); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if deleteOpts.GracePeriodSeconds == nil || *deleteOpts.GracePeriodSeconds != 30 {
		t.Fatalf("delete grace period = %v, want 30", deleteOpts.GracePeriodSeconds)
	}
	if _, err := client.CoreV1().Pods(testNamespace).Get(context.Background(), "lobby-9", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("pod still exists after Stop: %v", err)
	}
}

func TestStopOnMissingPodReturnsErrNotFound(t *testing.T) {
	d, _ := newTestDriver(t)

	err := d.Stop(context.Background(), "does-not-exist", "", 10*time.Second)
	if !errors.Is(err, driver.ErrNotFound) {
		t.Fatalf("Stop error = %v, want driver.ErrNotFound", err)
	}
}

func TestKillDeletesPodWithoutGracePeriod(t *testing.T) {
	pod := managedPod("lobby-10", "lobby", "game", "dynamic", 1)
	d, client := newTestDriver(t, pod)

	var deleteOpts metav1.DeleteOptions
	client.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleteOpts = action.(k8stesting.DeleteActionImpl).DeleteOptions
		return false, nil, nil
	})

	if err := d.Kill(context.Background(), "lobby-10"); err != nil {
		t.Fatalf("Kill returned error: %v", err)
	}
	if deleteOpts.GracePeriodSeconds == nil || *deleteOpts.GracePeriodSeconds != 0 {
		t.Fatalf("delete grace period = %v, want 0", deleteOpts.GracePeriodSeconds)
	}
}

// managedPod is a pod as Grus creates it, without status.
func managedPod(name, pool, role, mode string, generation int64) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				driver.LabelManagedBy: driver.ManagedByValue,
				driver.LabelPool:      pool,
				driver.LabelInstance:  name,
				driver.LabelRole:      role,
			},
			Annotations: map[string]string{
				driver.AnnotationGeneration:  strconv.FormatInt(generation, 10),
				driver.AnnotationTokenSHA256: "hash-" + name,
				driver.AnnotationMode:        mode,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "server",
				Ports: []corev1.ContainerPort{{Name: "minecraft", ContainerPort: 25565, Protocol: corev1.ProtocolTCP}},
			}},
		},
	}
}

// listOne runs List on a fake holding exactly pod and returns its single observation.
func listOne(t *testing.T, pod *corev1.Pod) driver.Observed {
	t.Helper()
	d, client := newTestDriver(t)
	if _, err := client.CoreV1().Pods(testNamespace).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	// The fake tracker keeps status as given on create; read it back to be sure nothing was dropped.
	stored, err := client.CoreV1().Pods(testNamespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if !reflect.DeepEqual(stored.Status, pod.Status) {
		t.Fatalf("fake dropped pod status: got %+v want %+v", stored.Status, pod.Status)
	}
	obs, err := d.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("List returned %d observations, want 1", len(obs))
	}
	return obs[0]
}

func TestStartPrefersEffectiveSpecOverV1PoolFields(t *testing.T) {
	d, _ := newTestDriver(t)
	spec := testStartSpec(false)
	spec.Effective = model.EffectiveSpec{
		Driver: "kubernetes", Image: "registry.example/paper:9", Command: []string{"run"}, Args: []string{"--x"},
		Port: 30000, GracePeriod: model.Duration(45 * time.Second), Labels: map[string]string{"from": "group"},
	}
	pod := startPod(t, d, spec)

	server := containerNamed(t, pod.Spec.Containers, "server")
	if server.Image != "registry.example/paper:9" || !reflect.DeepEqual(server.Command, []string{"run"}) ||
		!reflect.DeepEqual(server.Args, []string{"--x"}) || server.Ports[0].ContainerPort != 30000 {
		t.Errorf("server container ignores the effective spec: %+v", server)
	}
	if pod.Labels["from"] != "group" {
		t.Errorf("labels = %v, want the effective labels", pod.Labels)
	}
	if pod.Spec.TerminationGracePeriodSeconds == nil || *pod.Spec.TerminationGracePeriodSeconds != 45 {
		t.Errorf("termination grace = %v, want 45", pod.Spec.TerminationGracePeriodSeconds)
	}
}
