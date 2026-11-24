// Package kubernetes runs Grus instances as bare pods in one namespace. There are no Deployments,
// StatefulSets or CRDs: the controller creates the pod, reads its state back and removes it again.
package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

const (
	serverContainer    = "server"
	fetchContainer     = "fetch"
	dataVolume         = "data"
	dataMountPath      = "/data"
	portName           = "minecraft"
	tokenSecretPrefix  = "grus-token-"
	tokenKey           = "token"
	managedSelector    = driver.LabelManagedBy + "=" + driver.ManagedByValue
	stopCommandTimeout = 5 * time.Second
	runAsUser          = int64(10001)
)

// Config is the static configuration of the driver, taken from the controller's environment.
type Config struct {
	Namespace            string
	HostID               string // e.g. "k8s-grus-dev"
	SelfImage            string // grus image, used for the template fetch init container
	ImagePullSecrets     []string
	DefaultPriorityClass string // used when PoolSpec.PriorityClassName is empty
	NodeSelector         map[string]string
	PodLabels            map[string]string // extra labels on every pod
	RestConfig           *rest.Config      // needed only for Command (attach); may be nil in tests
}

// Driver implements driver.Driver for bare Kubernetes pods. It keeps no state of its own.
type Driver struct {
	client kubernetes.Interface
	cfg    Config
}

// New returns a driver that works in cfg.Namespace through client.
func New(client kubernetes.Interface, cfg Config) *Driver {
	return &Driver{client: client, cfg: cfg}
}

// Name returns "kubernetes".
func (d *Driver) Name() string { return "kubernetes" }

// Host returns the host id this driver represents.
func (d *Driver) Host() string { return d.cfg.HostID }

// Start creates the pod and then the token secret that the pod references. If the secret cannot be
// created, the pod is removed again so no instance runs without its token.
func (d *Driver) Start(ctx context.Context, spec driver.StartSpec) (string, error) {
	pod, err := d.podFor(spec)
	if err != nil {
		return "", err
	}
	created, err := d.client.CoreV1().Pods(d.cfg.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create pod %s: %w", pod.Name, err)
	}
	secret := tokenSecret(spec, created)
	if _, err := d.client.CoreV1().Secrets(d.cfg.Namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		if delErr := d.client.CoreV1().Pods(d.cfg.Namespace).Delete(ctx, created.Name, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
			return "", fmt.Errorf("create token secret for pod %s: %w (removing the pod failed too: %v)", created.Name, err, delErr)
		}
		return "", fmt.Errorf("create token secret for pod %s: %w", created.Name, err)
	}
	return created.Name, nil
}

// Stop optionally sends the stop command over stdin, then deletes the pod with the grace period.
// The stop command is best effort: its failure does not prevent the delete.
func (d *Driver) Stop(ctx context.Context, ref string, stopCommand string, grace time.Duration) error {
	if stopCommand != "" && d.cfg.RestConfig != nil {
		cmdCtx, cancel := context.WithTimeout(ctx, stopCommandTimeout)
		if err := d.Command(cmdCtx, ref, stopCommand); err != nil {
			slog.Debug("stop command failed, deleting pod anyway", "ref", ref, "err", err)
		}
		cancel()
	}
	seconds := int64(grace / time.Second)
	return d.delete(ctx, ref, &seconds)
}

// Kill deletes the pod without a grace period.
func (d *Driver) Kill(ctx context.Context, ref string) error {
	zero := int64(0)
	return d.delete(ctx, ref, &zero)
}

// List maps every pod labelled as managed by Grus to an observation.
func (d *Driver) List(ctx context.Context) ([]driver.Observed, error) {
	list, err := d.client.CoreV1().Pods(d.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: managedSelector})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	out := make([]driver.Observed, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, observe(&list.Items[i]))
	}
	return out, nil
}

// Logs streams the log of the server container.
func (d *Driver) Logs(ctx context.Context, ref string, tail int, follow bool) (io.ReadCloser, error) {
	opts := &corev1.PodLogOptions{Container: serverContainer, Follow: follow}
	if tail > 0 {
		lines := int64(tail)
		opts.TailLines = &lines
	}
	stream, err := d.client.CoreV1().Pods(d.cfg.Namespace).GetLogs(ref, opts).Stream(ctx)
	if err != nil {
		return nil, mapNotFound(ref, err)
	}
	return stream, nil
}

// Command writes one line to the stdin of the server container through pods/attach.
func (d *Driver) Command(ctx context.Context, ref string, line string) error {
	if d.cfg.RestConfig == nil {
		return errors.New("kubernetes: command needs a rest config")
	}
	cs, err := kubernetes.NewForConfig(d.cfg.RestConfig)
	if err != nil {
		return fmt.Errorf("kubernetes: build client for attach: %w", err)
	}
	req := cs.CoreV1().RESTClient().Post().
		Namespace(d.cfg.Namespace).
		Resource("pods").
		Name(ref).
		SubResource("attach").
		VersionedParams(&corev1.PodAttachOptions{Container: serverContainer, Stdin: true}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(d.cfg.RestConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("kubernetes: attach to %s: %w", ref, err)
	}
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: strings.NewReader(line + "\n")}); err != nil {
		return fmt.Errorf("kubernetes: write to %s: %w", ref, err)
	}
	return nil
}

func (d *Driver) delete(ctx context.Context, ref string, grace *int64) error {
	err := d.client.CoreV1().Pods(d.cfg.Namespace).Delete(ctx, ref, metav1.DeleteOptions{GracePeriodSeconds: grace})
	return mapNotFound(ref, err)
}

func mapNotFound(ref string, err error) error {
	if err == nil {
		return nil
	}
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("%s: %w", ref, driver.ErrNotFound)
	}
	return fmt.Errorf("kubernetes: %s: %w", ref, err)
}

// podFor builds the bare pod for one instance.
func (d *Driver) podFor(spec driver.StartSpec) (*corev1.Pod, error) {
	pool := spec.Pool.Spec.WithDefaults()
	grace := time.Duration(pool.Lifecycle.GracePeriod)
	graceSeconds := int64(grace / time.Second)
	poolName := spec.Instance.Pool

	resources, err := serverResources(pool.Resources)
	if err != nil {
		return nil, err
	}

	server := corev1.Container{
		Name:            serverContainer,
		Image:           pool.Image,
		Command:         pool.Command,
		Args:            pool.Args,
		WorkingDir:      dataMountPath,
		Stdin:           true,
		TTY:             false,
		Env:             serverEnv(spec, pool),
		Ports:           []corev1.ContainerPort{{Name: portName, ContainerPort: int32(pool.Port), Protocol: corev1.ProtocolTCP}},
		Resources:       resources,
		VolumeMounts:    []corev1.VolumeMount{dataMount()},
		SecurityContext: containerSecurityContext(),
	}

	var initContainers []corev1.Container
	if len(spec.Templates) > 0 {
		initContainers = append(initContainers, d.fetchContainer(spec))
	}

	priority := pool.PriorityClassName
	if priority == "" {
		priority = d.cfg.DefaultPriorityClass
	}

	var pullSecrets []corev1.LocalObjectReference
	for _, name := range d.cfg.ImagePullSecrets {
		pullSecrets = append(pullSecrets, corev1.LocalObjectReference{Name: name})
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Instance.ID,
			Namespace: d.cfg.Namespace,
			Labels:    d.labels(spec, poolName, pool),
			Annotations: map[string]string{
				driver.AnnotationGeneration:  strconv.FormatInt(spec.Instance.PoolGeneration, 10),
				driver.AnnotationTokenSHA256: spec.Instance.TokenHash,
				driver.AnnotationMode:        string(pool.Mode),
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			AutomountServiceAccountToken:  ptr(false),
			EnableServiceLinks:            ptr(false),
			TerminationGracePeriodSeconds: &graceSeconds,
			PriorityClassName:             priority,
			NodeSelector:                  d.cfg.NodeSelector,
			ImagePullSecrets:              pullSecrets,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr(true),
				RunAsUser:    ptr(runAsUser),
				RunAsGroup:   ptr(runAsUser),
				FSGroup:      ptr(runAsUser),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes:        []corev1.Volume{{Name: dataVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
			InitContainers: initContainers,
			Containers:     []corev1.Container{server},
		},
	}, nil
}

// fetchContainer downloads the resolved templates into /data before the server starts.
func (d *Driver) fetchContainer(spec driver.StartSpec) corev1.Container {
	refs := make([]string, 0, len(spec.Templates))
	for _, t := range spec.Templates {
		refs = append(refs, t.Name+"@"+t.Digest)
	}
	return corev1.Container{
		Name:    fetchContainer,
		Image:   d.cfg.SelfImage,
		Command: []string{"/grus", "fetch"},
		Args:    []string{"--dest", dataMountPath},
		Env: []corev1.EnvVar{
			{Name: "GRUS_URL", Value: spec.Env["GRUS_URL"]},
			{Name: "GRUS_FETCH_TEMPLATES", Value: strings.Join(refs, ",")},
			tokenEnv(spec.Instance.ID),
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
				corev1.ResourceCPU:    resource.MustParse("10m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
		VolumeMounts:    []corev1.VolumeMount{dataMount()},
		SecurityContext: containerSecurityContext(),
	}
}

// serverEnv is the spec env sorted by key, then the token, then the pool's secret env.
func serverEnv(spec driver.StartSpec, pool model.PoolSpec) []corev1.EnvVar {
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]corev1.EnvVar, 0, len(keys)+1+len(pool.SecretEnv))
	for _, k := range keys {
		env = append(env, corev1.EnvVar{Name: k, Value: spec.Env[k]})
	}
	env = append(env, tokenEnv(spec.Instance.ID))
	for _, s := range pool.SecretEnv {
		env = append(env, corev1.EnvVar{
			Name: s.Name,
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: s.Secret},
				Key:                  s.Key,
			}},
		})
	}
	return env
}

// tokenEnv injects GRUS_TOKEN from the instance's token secret.
func tokenEnv(instanceID string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: "GRUS_TOKEN",
		ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: tokenSecretPrefix + instanceID},
			Key:                  tokenKey,
		}},
	}
}

// serverResources sets memory request = limit, and a CPU request without a CPU limit.
func serverResources(r model.Resources) (corev1.ResourceRequirements, error) {
	mem := resource.NewQuantity(int64(r.MemoryMiB)*1024*1024, resource.BinarySI)
	req := corev1.ResourceList{corev1.ResourceMemory: *mem}
	if r.CPU != "" {
		cpu, err := resource.ParseQuantity(r.CPU)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("resources.cpu %q: %w", r.CPU, err)
		}
		req[corev1.ResourceCPU] = cpu
	}
	return corev1.ResourceRequirements{
		Requests: req,
		Limits:   corev1.ResourceList{corev1.ResourceMemory: *mem},
	}, nil
}

func (d *Driver) labels(spec driver.StartSpec, poolName string, pool model.PoolSpec) map[string]string {
	labels := make(map[string]string, len(d.cfg.PodLabels)+len(pool.Labels)+4)
	for k, v := range d.cfg.PodLabels {
		labels[k] = v
	}
	for k, v := range pool.Labels {
		labels[k] = v
	}
	// Driver labels win over user labels, so adoption by label keeps working.
	labels[driver.LabelManagedBy] = driver.ManagedByValue
	labels[driver.LabelPool] = poolName
	labels[driver.LabelInstance] = spec.Instance.ID
	labels[driver.LabelRole] = string(spec.Instance.Role)
	return labels
}

// tokenSecret holds the plain token of one instance. It is owned by the pod, so Kubernetes garbage
// collects it together with the pod.
func tokenSecret(spec driver.StartSpec, pod *corev1.Pod) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tokenSecretPrefix + spec.Instance.ID,
			Namespace: pod.Namespace,
			Labels: map[string]string{
				driver.LabelManagedBy: driver.ManagedByValue,
				driver.LabelPool:      spec.Instance.Pool,
				driver.LabelInstance:  spec.Instance.ID,
				driver.LabelRole:      string(spec.Instance.Role),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "Pod",
				Name:       pod.Name,
				UID:        pod.UID,
				Controller: ptr(true),
			}},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{tokenKey: []byte(spec.Token)},
	}
}

func dataMount() corev1.VolumeMount {
	return corev1.VolumeMount{Name: dataVolume, MountPath: dataMountPath}
}

// containerSecurityContext is the restrictive context every Grus container runs with (N6).
func containerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		RunAsNonRoot:             ptr(true),
		ReadOnlyRootFilesystem:   ptr(false),
	}
}

func ptr[T any](v T) *T { return &v }

// observe maps a pod to the driver's view of an instance.
func observe(pod *corev1.Pod) driver.Observed {
	generation, _ := strconv.ParseInt(pod.Annotations[driver.AnnotationGeneration], 10, 64)
	phase, reason := phaseOf(pod)
	if pod.DeletionTimestamp != nil && (phase == driver.PhaseRunning || phase == driver.PhasePending) {
		reason = "terminating"
	}
	o := driver.Observed{
		Ref:            pod.Name,
		InstanceID:     pod.Labels[driver.LabelInstance],
		Pool:           pod.Labels[driver.LabelPool],
		Role:           model.Role(pod.Labels[driver.LabelRole]),
		Mode:           model.Mode(pod.Annotations[driver.AnnotationMode]),
		PoolGeneration: generation,
		TokenHash:      pod.Annotations[driver.AnnotationTokenSHA256],
		Phase:          phase,
		Reason:         reason,
		StartedAt:      pod.CreationTimestamp.Time,
	}
	if pod.Status.PodIP != "" {
		if port, ok := serverPort(pod); ok {
			o.Address = fmt.Sprintf("%s:%d", pod.Status.PodIP, port)
		}
	}
	return o
}

func serverPort(pod *corev1.Pod) (int32, bool) {
	for _, c := range pod.Spec.Containers {
		if c.Name == serverContainer && len(c.Ports) > 0 {
			return c.Ports[0].ContainerPort, true
		}
	}
	return 0, false
}

// phaseOf derives the Grus phase from pod and container status.
func phaseOf(pod *corev1.Pod) (driver.Phase, string) {
	for _, cs := range pod.Status.InitContainerStatuses {
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			return driver.PhaseFailed, "template fetch failed: " + describeTerminated(t)
		}
	}

	server := containerStatus(pod, serverContainer)
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		reason := pod.Status.Reason
		if server != nil && server.State.Terminated != nil {
			reason = describeTerminated(server.State.Terminated)
		}
		return driver.PhaseExited, reason
	}

	if reason, ok := waitingReason(pod, imagePullReasons); ok {
		return driver.PhaseFailed, reason
	}
	if reason, ok := waitingReason(pod, nil); ok {
		return driver.PhasePending, reason
	}
	if server != nil && server.State.Running != nil {
		return driver.PhaseRunning, ""
	}
	return driver.PhasePending, ""
}

var imagePullReasons = map[string]bool{
	"ErrImagePull":     true,
	"ImagePullBackOff": true,
	"InvalidImageName": true,
}

// waitingReason returns the first waiting container of init or main containers. With a nil filter
// every waiting reason matches; otherwise only the reasons in the filter do.
func waitingReason(pod *corev1.Pod, filter map[string]bool) (string, bool) {
	all := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, cs := range all {
		w := cs.State.Waiting
		if w == nil || w.Reason == "" {
			continue
		}
		if filter != nil && !filter[w.Reason] {
			continue
		}
		if w.Message != "" {
			return w.Reason + ": " + w.Message, true
		}
		return w.Reason, true
	}
	return "", false
}

func containerStatus(pod *corev1.Pod, name string) *corev1.ContainerStatus {
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == name {
			return &pod.Status.ContainerStatuses[i]
		}
	}
	return nil
}

func describeTerminated(t *corev1.ContainerStateTerminated) string {
	reason := t.Reason
	if reason == "" {
		reason = "Error"
	}
	s := fmt.Sprintf("%s (exit code %d)", reason, t.ExitCode)
	if t.Message != "" {
		s += ": " + t.Message
	}
	return s
}

// Compile-time check that the driver implements the contract.
var _ driver.Driver = (*Driver)(nil)
