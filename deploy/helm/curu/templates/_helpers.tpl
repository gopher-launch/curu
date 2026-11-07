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
{{- include "curu.postgresSecretName" . -}}
{{- else -}}
{{- .Values.database.external.runtimeSecret.name -}}
{{- end -}}
{{- end -}}

{{- define "curu.runtimeSecretKey" -}}
{{- if .Values.database.bundled.enabled -}}
runtime-url
{{- else -}}
{{- .Values.database.external.runtimeSecret.key -}}
{{- end -}}
{{- end -}}

{{- define "curu.migrationSecret" -}}
{{- if .Values.database.bundled.enabled -}}{{ include "curu.postgresSecretName" . }}{{- else -}}{{ .Values.database.external.migrationSecret.name }}{{- end -}}
{{- end -}}

{{- define "curu.migrationSecretKey" -}}
{{- if .Values.database.bundled.enabled -}}migration-url{{- else -}}{{ .Values.database.external.migrationSecret.key }}{{- end -}}
{{- end -}}

{{- define "curu.initializerSecret" -}}
{{- if .Values.database.bundled.enabled -}}{{ include "curu.postgresSecretName" . }}{{- else -}}{{ .Values.database.external.initializerSecret.name }}{{- end -}}
{{- end -}}

{{- define "curu.initializerSecretKey" -}}
{{- if .Values.database.bundled.enabled -}}initializer-url{{- else -}}{{ .Values.database.external.initializerSecret.key }}{{- end -}}
{{- end -}}

{{- define "curu.runtimeRole" -}}
{{- if .Values.database.bundled.enabled -}}{{ .Values.database.bundled.names.runtimeRole }}{{- else -}}{{ .Values.database.external.runtimeRole }}{{- end -}}
{{- end -}}

{{- define "curu.initializerRole" -}}
{{- if .Values.database.bundled.enabled -}}{{ .Values.database.bundled.names.initializerRole }}{{- else -}}{{ .Values.database.external.initializerRole }}{{- end -}}
{{- end -}}

{{- define "curu.postgresName" -}}
{{- printf "%s-postgres" (include "curu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "curu.postgresSecretName" -}}
{{- default (printf "%s-credentials" (include "curu.postgresName" .)) .Values.database.bundled.credentialsSecret.existingName -}}
{{- end -}}

{{- define "curu.postgresImage" -}}
{{- printf "%s:%s@%s" .Values.database.bundled.image.repository .Values.database.bundled.image.tag .Values.database.bundled.image.digest -}}
{{- end -}}

{{- define "curu.validate" -}}
{{- if ne .Values.owner.issuer .Values.trust.issuer -}}
{{- fail "owner.issuer must exactly match trust.issuer" -}}
{{- end -}}
{{- if and (or .Values.dataLifecycle.migrate (ne .Values.dataLifecycle.initialize "none")) .Values.runtime.enabled -}}
{{- fail "runtime.enabled must be false while a data lifecycle operation is selected" -}}
{{- end -}}
{{- if and .Values.database.external.enabled (eq (include "curu.runtimeSecret" .) "") -}}
{{- fail "the selected database mode requires a non-empty runtimeSecret.name" -}}
{{- end -}}
{{- if .Values.database.bundled.enabled -}}
{{- range $name, $value := .Values.database.bundled.names -}}
{{- if not (regexMatch "^[a-z_][a-z0-9_]{0,62}$" $value) -}}
{{- fail (printf "database.bundled.names.%s must be a lowercase PostgreSQL identifier" $name) -}}
{{- end -}}
{{- end -}}
{{- $roles := list .Values.database.bundled.names.migrationRole .Values.database.bundled.names.initializerRole .Values.database.bundled.names.runtimeRole .Values.database.bundled.names.keycloakRole -}}
{{- if ne (len $roles) (len (uniq $roles)) -}}
{{- fail "database.bundled service role names must be pairwise distinct" -}}
{{- end -}}
{{- range $role := $roles -}}
{{- if or (eq $role "postgres") (eq $role "public") (hasPrefix "pg_" $role) -}}
{{- fail (printf "database.bundled service role name %q is reserved by PostgreSQL" $role) -}}
{{- end -}}
{{- end -}}
{{- if eq .Values.database.bundled.names.curuDatabase .Values.database.bundled.names.keycloakDatabase -}}
{{- fail "database.bundled Curu and Keycloak database names must be distinct" -}}
{{- end -}}
{{- range $database := list .Values.database.bundled.names.curuDatabase .Values.database.bundled.names.keycloakDatabase -}}
{{- if has $database (list "postgres" "template0" "template1") -}}
{{- fail (printf "database.bundled database name %q is reserved by PostgreSQL" $database) -}}
{{- end -}}
{{- end -}}
{{- if ne (.Values.database.bundled.service.port | int) 5432 -}}
{{- fail "database.bundled.service.port is fixed at 5432" -}}
{{- end -}}
{{- end -}}
{{- if .Values.database.external.enabled -}}
{{- if or (eq .Values.database.external.host "") (eq .Values.database.external.curuDatabase "") (eq .Values.database.external.keycloakDatabase "") -}}
{{- fail "external database mode requires explicit host, curuDatabase, and keycloakDatabase values" -}}
{{- end -}}
{{- range $secret := list .Values.database.external.administrationSecret .Values.database.external.migrationSecret .Values.database.external.initializerSecret .Values.database.external.runtimeSecret .Values.database.external.keycloakSecret -}}
{{- if eq $secret.name "" -}}
{{- fail "external database mode requires all five scoped Secret references" -}}
{{- end -}}
{{- end -}}
{{- if and (has .Values.database.external.tls.mode (list "verify-ca" "verify-full")) (eq .Values.database.external.tls.caSecret.name "") -}}
{{- fail "external database TLS verification requires tls.caSecret.name" -}}
{{- end -}}
{{- end -}}
{{- if hasKey .Values.podLabels "app.kubernetes.io/name" -}}
{{- fail "podLabels must not override chart-owned label app.kubernetes.io/name" -}}
{{- end -}}
{{- if hasKey .Values.podLabels "app.kubernetes.io/instance" -}}
{{- fail "podLabels must not override chart-owned label app.kubernetes.io/instance" -}}
{{- end -}}
{{- if hasKey .Values.podLabels "app.kubernetes.io/component" -}}
{{- fail "podLabels must not override chart-owned label app.kubernetes.io/component" -}}
{{- end -}}
{{- end -}}
