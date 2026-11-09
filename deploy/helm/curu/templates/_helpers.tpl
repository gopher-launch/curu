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

{{- define "curu.keycloakName" -}}
{{- printf "%s-keycloak" (include "curu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "curu.keycloakSecretName" -}}
{{- default (printf "%s-credentials" (include "curu.keycloakName" .)) .Values.identity.bundled.credentialsSecret.existingName -}}
{{- end -}}

{{- define "curu.keycloakImage" -}}
{{- printf "%s:%s@%s" .Values.identity.bundled.image.repository .Values.identity.bundled.image.tag .Values.identity.bundled.image.digest -}}
{{- end -}}

{{- define "curu.edgeName" -}}curu-edge{{- end -}}

{{- define "curu.edgeImage" -}}
{{- printf "%s:%s@%s" .Values.edge.image.repository .Values.edge.image.tag .Values.edge.image.digest -}}
{{- end -}}

{{- define "curu.oauth2ProxyName" -}}
{{- printf "%s-oauth2-proxy" (include "curu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "curu.oauth2ProxyImage" -}}
{{- printf "%s:%s@%s" .Values.browserAuth.image.repository .Values.browserAuth.image.tag .Values.browserAuth.image.digest -}}
{{- end -}}

{{- define "curu.browserAuthSecretName" -}}
{{- default (printf "%s-session" (include "curu.oauth2ProxyName" .)) .Values.browserAuth.sessionSecret.existingName -}}
{{- end -}}

{{- define "curu.browserClientID" -}}
{{- if .Values.identity.bundled.enabled -}}{{ .Values.identity.bundled.client.id }}{{- else -}}{{ .Values.identity.external.client.id }}{{- end -}}
{{- end -}}

{{- define "curu.browserClientSecretName" -}}
{{- if .Values.identity.bundled.enabled -}}{{ include "curu.keycloakSecretName" . }}{{- else -}}{{ .Values.identity.external.client.secret.name }}{{- end -}}
{{- end -}}

{{- define "curu.browserClientSecretKey" -}}
{{- if .Values.identity.bundled.enabled -}}client-secret{{- else -}}{{ .Values.identity.external.client.secret.key }}{{- end -}}
{{- end -}}

{{- define "curu.keycloakDatabaseBootstrapName" -}}
{{- $base := printf "%s-database-bootstrap" (include "curu.keycloakName" .) | trunc 54 | trimSuffix "-" -}}
{{- $revision := printf "%v" .Release.Revision | sha256sum | trunc 8 -}}
{{- printf "%s-%s" $base $revision -}}
{{- end -}}

{{- define "curu.keycloakDatabaseSecret" -}}
{{- if .Values.database.bundled.enabled -}}{{ include "curu.postgresSecretName" . }}{{- else -}}{{ .Values.database.external.keycloakSecret.name }}{{- end -}}
{{- end -}}

{{- define "curu.keycloakDatabaseSecretKey" -}}
{{- if .Values.database.bundled.enabled -}}keycloak-jdbc-url{{- else -}}{{ .Values.database.external.keycloakSecret.key }}{{- end -}}
{{- end -}}

{{- define "curu.validate" -}}
{{- if .Values.edge.enabled -}}
{{- $expectedOrigin := printf "https://%s:%d" .Values.edge.applicationHost (.Values.edge.publicPort | int) -}}
{{- if ne .Values.edge.publicOrigin $expectedOrigin -}}
{{- fail "edge.publicOrigin must be the exact HTTPS application host and public port" -}}
{{- end -}}
{{- if ne (.Values.edge.service.port | int) (.Values.edge.publicPort | int) -}}
{{- fail "edge.service.port must equal edge.publicPort so public issuer names resolve in cluster" -}}
{{- end -}}
{{- if ne .Values.runtime.publicOrigin .Values.edge.publicOrigin -}}
{{- fail "runtime.publicOrigin must exactly match edge.publicOrigin" -}}
{{- end -}}
{{- if and .Values.identity.bundled.enabled (eq .Values.edge.applicationHost .Values.edge.identityHost) -}}
{{- fail "edge.applicationHost and edge.identityHost must differ in bundled identity mode" -}}
{{- end -}}
{{- if eq .Values.runtime.trustedProxyCIDRs "" -}}
{{- fail "edge mode requires explicit runtime.trustedProxyCIDRs for the immediate NGINX peer" -}}
{{- end -}}
{{- if ne .Values.runtime.loginURL (printf "%s/oauth2/start?rd=%s%%2Factivities" .Values.edge.publicOrigin (urlquery .Values.edge.publicOrigin)) -}}
{{- fail "runtime.loginURL must be the fixed edge sign-in URL for /activities" -}}
{{- end -}}
{{- if ne .Values.runtime.logoutURL (printf "%s/oauth2/sign_out?rd=%s%%2F" .Values.edge.publicOrigin (urlquery .Values.edge.publicOrigin)) -}}
{{- fail "runtime.logoutURL must be the fixed edge session-termination URL" -}}
{{- end -}}
{{- if and .Values.identity.bundled.enabled (ne .Values.edge.identityHost (trimPrefix "https://" (regexReplaceAll ":[0-9]+$" .Values.identity.bundled.hostname ""))) -}}
{{- fail "edge.identityHost must match the bundled identity hostname" -}}
{{- end -}}
{{- end -}}
{{- if and .Values.edge.enabled (or .Values.dataLifecycle.migrate (ne .Values.dataLifecycle.initialize "none")) -}}
{{- fail "edge.enabled must be false while a data lifecycle operation is selected" -}}
{{- end -}}
{{- if ne .Values.owner.issuer .Values.trust.issuer -}}
{{- fail "owner.issuer must exactly match trust.issuer" -}}
{{- end -}}
{{- if and .Values.identity.bundled.enabled (ne .Values.trust.issuer (printf "%s/realms/%s" (trimSuffix "/" .Values.identity.bundled.hostname) .Values.identity.bundled.realm)) -}}
{{- fail "bundled identity requires trust.issuer to equal identity.bundled.hostname plus /realms/<realm>" -}}
{{- end -}}
{{- if and .Values.identity.bundled.enabled (ne .Values.trust.jwksURL (printf "%s/realms/%s/protocol/openid-connect/certs" (trimSuffix "/" .Values.identity.bundled.hostname) .Values.identity.bundled.realm)) -}}
{{- fail "bundled identity requires trust.jwksURL to equal the realm JWKS endpoint" -}}
{{- end -}}
{{- if and .Values.identity.bundled.enabled (ne .Values.trust.audience .Values.identity.bundled.audience) -}}
{{- fail "bundled identity audience must exactly match trust.audience" -}}
{{- end -}}
{{- if and .Values.identity.bundled.enabled (ne .Values.owner.subject .Values.identity.bundled.users.owner.subject) -}}
{{- fail "bundled identity owner subject must exactly match owner.subject" -}}
{{- end -}}
{{- if and .Values.identity.bundled.enabled (eq .Values.identity.bundled.users.owner.subject .Values.identity.bundled.users.other.subject) -}}
{{- fail "bundled identity development-user subjects must be distinct" -}}
{{- end -}}
{{- if and .Values.identity.external.enabled (or (eq .Values.identity.external.client.id "") (eq .Values.identity.external.client.secret.name "")) -}}
{{- fail "external identity mode requires an explicit browser client and client Secret reference" -}}
{{- end -}}
{{- if eq .Values.identity.caTrust.configMapName "" -}}
{{- fail "identity.caTrust.configMapName is required for verified issuer/JWKS HTTPS" -}}
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
{{- if eq .Values.database.external.initializerRole .Values.database.external.runtimeRole -}}
{{- fail "database.external initializerRole and runtimeRole must be distinct" -}}
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
