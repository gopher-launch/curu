{{- define "curu.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "curu.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "curu.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "curu.labels" -}}
app.kubernetes.io/name: {{ include "curu.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "curu.selectorLabels" -}}
app.kubernetes.io/name: {{ include "curu.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "curu.runtimeSecret" -}}
{{- if .Values.database.bundled.enabled -}}
{{- .Values.database.bundled.runtimeSecret.name -}}
{{- else -}}
{{- .Values.database.external.runtimeSecret.name -}}
{{- end -}}
{{- end -}}

{{- define "curu.runtimeSecretKey" -}}
{{- if .Values.database.bundled.enabled -}}
{{- .Values.database.bundled.runtimeSecret.key -}}
{{- else -}}
{{- .Values.database.external.runtimeSecret.key -}}
{{- end -}}
{{- end -}}

{{- define "curu.validate" -}}
{{- if ne .Values.owner.issuer .Values.trust.issuer -}}
{{- fail "owner.issuer must exactly match trust.issuer" -}}
{{- end -}}
{{- if eq (include "curu.runtimeSecret" .) "" -}}
{{- fail "the selected database mode requires a non-empty runtimeSecret.name" -}}
{{- end -}}
{{- end -}}
