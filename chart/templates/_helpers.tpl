{{- define "kubecrux.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubecrux.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- include "kubecrux.name" . -}}
{{- end -}}
{{- end -}}

{{- define "kubecrux.labels" -}}
app.kubernetes.io/name: {{ include "kubecrux.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "kubecrux.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubecrux.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "kubecrux.postgresServiceName" -}}
{{- printf "%s-postgres" (include "kubecrux.fullname" .) -}}
{{- end -}}
