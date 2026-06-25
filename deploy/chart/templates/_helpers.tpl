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

{{- define "soha.postgresHost" -}}
{{- if .Values.postgres.enabled -}}
{{- include "soha.postgresServiceName" . -}}
{{- else -}}
{{- required "postgres.host is required when postgres.enabled=false" .Values.postgres.host -}}
{{- end -}}
{{- end -}}

{{- define "soha.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{- define "soha.secretValue" -}}
{{- $name := .name -}}
{{- $value := trim (default "" .value) -}}
{{- $existing := trim (default "" .existing) -}}
{{- $candidate := $value -}}
{{- if not $candidate -}}
{{- $candidate = $existing -}}
{{- end -}}
{{- $lower := lower $candidate -}}
{{- if or (eq $lower "change-me") (eq $lower "changeme") (eq $lower "dev-only-change-me") (eq $lower "demo-execution-runner-token") (eq $lower "dev-alert-webhook-token") (eq $lower "soha") (eq $lower "pgsql") (contains "replace-with" $lower) (contains "replace_with" $lower) (contains "placeholder" $lower) -}}
{{- fail (printf "%s must not use a demo or placeholder value" $name) -}}
{{- end -}}
{{- if $candidate -}}
{{- $candidate -}}
{{- else -}}
{{- randAlphaNum (default 48 .length) -}}
{{- end -}}
{{- end -}}

{{- define "soha.requiredSecretValue" -}}
{{- $name := .name -}}
{{- $value := include "soha.secretValue" . -}}
{{- if not (trim $value) -}}
{{- fail (printf "%s is required" $name) -}}
{{- end -}}
{{- $value -}}
{{- end -}}
