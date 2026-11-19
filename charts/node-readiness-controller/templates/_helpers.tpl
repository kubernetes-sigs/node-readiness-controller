{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "node-readiness-controller.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "node-readiness-controller.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "node-readiness-controller.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Expand the namespace of the release.
Allows overriding it for multi-namespace deployments in combined charts.
*/}}
{{- define "node-readiness-controller.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "node-readiness-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "node-readiness-controller.labels" -}}
app.kubernetes.io/name: {{ include "node-readiness-controller.name" . }}
helm.sh/chart: {{ include "node-readiness-controller.chart" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Values.commonLabels}}
{{ toYaml .Values.commonLabels }}
{{- end }}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "node-readiness-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "node-readiness-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "node-readiness-controller.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "node-readiness-controller.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Whether the health probe endpoint is served.
The controller disables it when --health-probe-bind-address is "0" or empty,
so the container port and probes must be omitted to match.
*/}}
{{- define "node-readiness-controller.healthProbeEnabled" -}}
{{- $addr := .Values.healthProbeBindAddress | toString | trim -}}
{{- if and (ne $addr "") (ne $addr "0") -}}
true
{{- end -}}
{{- end -}}

{{/*
Port taken from healthProbeBindAddress, so the container port and the
liveness/readiness probes follow the address the controller actually binds to.
Accepts ":8081", "0.0.0.0:8081" and "[::]:8081".
*/}}
{{- define "node-readiness-controller.healthProbePort" -}}
{{- $addr := .Values.healthProbeBindAddress | toString | trim -}}
{{- $match := regexFind ":[0-9]+$" $addr -}}
{{- if not $match -}}
{{- fail (printf "healthProbeBindAddress %q must end with \":<port>\"" $addr) -}}
{{- end -}}
{{- trimPrefix ":" $match -}}
{{- end -}}
