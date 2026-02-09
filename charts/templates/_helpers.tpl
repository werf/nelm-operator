{{/*
Expand the name of the chart.
*/}}
{{- define "nelm-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "nelm-operator.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "nelm-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nelm-operator.labels" -}}
helm.sh/chart: {{ include "nelm-operator.chart" . }}
{{ include "nelm-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "nelm-source-controller.labels" -}}
helm.sh/chart: {{ include "nelm-operator.chart" . }}
{{ include "nelm-source-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "nelm-helm-controller.labels" -}}
helm.sh/chart: {{ include "nelm-operator.chart" . }}
{{ include "nelm-helm-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nelm-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nelm-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "nelm-source-controller.selectorLabels" -}}
{{- include "nelm-operator.selectorLabels" $ }}
app.kubernetes.io/component: "nelm-source-controller"
{{- end }}

{{- define "nelm-helm-controller.selectorLabels" -}}
{{- include "nelm-operator.selectorLabels" $ }}
app.kubernetes.io/component: "helm-controller"
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "nelm-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "nelm-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}


