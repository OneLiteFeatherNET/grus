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
