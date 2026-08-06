{{/*
Expand the name of the chart.
*/}}
{{- define "grafana-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Build the normalized namespace list used by both RBAC and the controller.
*/}}
{{- define "grafana-operator.watchNamespaces" -}}
{{- if .Values.watchNamespaces -}}
{{- $namespaces := list -}}
{{- range splitList "," .Values.watchNamespaces -}}
{{- $namespace := trim . -}}
{{- if $namespace -}}
{{- $namespaces = append $namespaces $namespace -}}
{{- end -}}
{{- end -}}
{{- if eq (len $namespaces) 0 -}}
{{- fail "watchNamespaces must contain at least one non-empty namespace" -}}
{{- end -}}
{{- join "," (sortAlpha (uniq $namespaces)) -}}
{{- else if .Values.namespaceScope -}}
{{- include "grafana-operator.namespace" . -}}
{{- end -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "grafana-operator.fullname" -}}
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

{{/*
Allow the release namespace to be overridden
*/}}
{{- define "grafana-operator.namespace" -}}
{{ .Values.namespaceOverride | default .Release.Namespace }}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "grafana-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "grafana-operator.labels" -}}
helm.sh/chart: {{ include "grafana-operator.chart" . }}
{{ include "grafana-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "grafana-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "grafana-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "grafana-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "grafana-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
