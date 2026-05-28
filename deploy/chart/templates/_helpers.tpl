{{- define "soha.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "soha.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- include "soha.name" . -}}
{{- end -}}
{{- end -}}

{{- define "soha.labels" -}}
app.kubernetes.io/name: {{ include "soha.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "soha.selectorLabels" -}}
app.kubernetes.io/name: {{ include "soha.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "soha.postgresServiceName" -}}
{{- printf "%s-postgres" (include "soha.fullname" .) -}}
{{- end -}}
