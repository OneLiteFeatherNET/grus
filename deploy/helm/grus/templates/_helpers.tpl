{{/*
Chart name, overridable.
*/}}
{{- define "grus.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name. If the release name already contains the chart name,
the release name is used on its own.
*/}}
{{- define "grus.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "grus.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "grus.selectorLabels" -}}
app.kubernetes.io/name: {{ include "grus.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "grus.labels" -}}
helm.sh/chart: {{ include "grus.chart" . }}
{{ include "grus.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: grus
{{- end }}

{{/*
Controller image: the repository from values, the tag from values or, when empty, the chart's appVersion.
*/}}
{{- define "grus.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end }}

{{/*
The URL instances use to reach the controller: controller.publicURL, or the in-cluster Service URL of this release.
*/}}
{{- define "grus.publicURL" -}}
{{- if .Values.controller.publicURL -}}
{{- .Values.controller.publicURL -}}
{{- else -}}
{{- printf "http://%s.%s.svc.cluster.local:%d" (include "grus.fullname" .) .Release.Namespace (int .Values.service.port) -}}
{{- end -}}
{{- end }}

{{/*
A map rendered as a comma separated list of key=value pairs, keys sorted (Go templates range over maps in key order).
Call with the map itself.
*/}}
{{- define "grus.keyValueList" -}}
{{- $items := list -}}
{{- range $key, $value := . -}}
{{- $items = append $items (printf "%s=%s" $key (toString $value)) -}}
{{- end -}}
{{- join "," $items -}}
{{- end }}

{{/*
ConfigMap of one seed template. Call with a dict: (dict "root" $ "template" "lobby")
*/}}
{{- define "grus.seedConfigMapName" -}}
{{- printf "%s-seed-%s" (include "grus.fullname" .root) .template | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Pod-level hardening: no root, no privilege escalation path, RuntimeDefault seccomp.
The controller keeps its Kubernetes API token (it manages instance pods), so automount is set in the Deployment.
*/}}
{{- define "grus.podSecurity" -}}
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532
  seccompProfile:
    type: RuntimeDefault
{{- end }}

{{- define "grus.containerSecurity" -}}
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
{{- end }}

{{/*
Web UI (optional). Its name label differs from the controller's, so the controller Service (which selects on
name and instance) never routes traffic to the web pods.
*/}}
{{- define "grus.web.fullname" -}}
{{- printf "%s-web" (include "grus.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "grus.web.selectorLabels" -}}
app.kubernetes.io/name: {{ printf "%s-web" (include "grus.name" .) | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: web
{{- end }}

{{- define "grus.web.labels" -}}
helm.sh/chart: {{ include "grus.chart" . }}
{{ include "grus.web.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: grus
{{- end }}

{{/*
Web UI image: the repository from web.image, the tag from web.image.tag or, when empty, the chart's appVersion.
*/}}
{{- define "grus.web.image" -}}
{{- printf "%s:%s" .Values.web.image.repository (default .Chart.AppVersion .Values.web.image.tag) -}}
{{- end }}
