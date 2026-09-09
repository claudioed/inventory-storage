{{/*
Expand the name of the chart.
*/}}
{{- define "inventory-storage.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "inventory-storage.fullname" -}}
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
Chart name and version as used by the chart label.
*/}}
{{- define "inventory-storage.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "inventory-storage.labels" -}}
helm.sh/chart: {{ include "inventory-storage.chart" . }}
{{ include "inventory-storage.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "inventory-storage.selectorLabels" -}}
app.kubernetes.io/name: {{ include "inventory-storage.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "inventory-storage.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "inventory-storage.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the Secret holding DATABASE_URL, when the chart creates its own.
*/}}
{{- define "inventory-storage.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- include "inventory-storage.fullname" . }}-database
{{- end }}
{{- end }}

{{/*
Fully qualified name of the analytics projector deployment (ADR-0011).
*/}}
{{- define "inventory-storage.projectorFullname" -}}
{{- include "inventory-storage.fullname" . }}-projector
{{- end }}

{{/*
Fully qualified name of the analytics reports deployment/service (ADR-0011).
*/}}
{{- define "inventory-storage.reportsFullname" -}}
{{- include "inventory-storage.fullname" . }}-reports
{{- end }}

{{/*
Name of the Secret holding the analytics DSNs, when the chart creates its own.
*/}}
{{- define "inventory-storage.analyticsSecretName" -}}
{{- if .Values.analytics.database.existingSecret }}
{{- .Values.analytics.database.existingSecret }}
{{- else }}
{{- include "inventory-storage.fullname" . }}-analytics
{{- end }}
{{- end }}

{{/*
Fully qualified name of the MCP server deployment/service (ADR-0008).
*/}}
{{- define "inventory-storage.mcpFullname" -}}
{{- include "inventory-storage.fullname" . }}-mcp
{{- end }}

{{/*
Name of the Secret holding the MCP bearer keys, when the chart creates its own.
*/}}
{{- define "inventory-storage.mcpSecretName" -}}
{{- if .Values.mcp.existingSecret }}
{{- .Values.mcp.existingSecret }}
{{- else }}
{{- include "inventory-storage.fullname" . }}-mcp
{{- end }}
{{- end }}

{{/*
Name of the Secret holding the REST bearer keys (ADR-0014), when the chart
creates its own.
*/}}
{{- define "inventory-storage.authSecretName" -}}
{{- if .Values.auth.existingSecret }}
{{- .Values.auth.existingSecret }}
{{- else }}
{{- include "inventory-storage.fullname" . }}-auth
{{- end }}
{{- end }}

{{/*
True when the REST auth Secret refs should be rendered on a pod: either the
chart owns a key or an existing Secret was named.
*/}}
{{- define "inventory-storage.authEnabled" -}}
{{- if or .Values.auth.readKey .Values.auth.readWriteKey .Values.facilityLayout.apiKey .Values.auth.existingSecret -}}true{{- end -}}
{{- end }}

{{/*
The REST auth env block shared by the main and reports containers: AUTH_MODE
always (from the ConfigMap), the key refs only when a Secret exists. Each key
is optional so a Secret may carry just one of them.
*/}}
{{- define "inventory-storage.authEnv" -}}
- name: AUTH_MODE
  valueFrom:
    configMapKeyRef:
      name: {{ include "inventory-storage.fullname" . }}
      key: AUTH_MODE
{{- if include "inventory-storage.authEnabled" . }}
- name: API_READ_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "inventory-storage.authSecretName" . }}
      key: API_READ_KEY
      optional: true
- name: API_READWRITE_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "inventory-storage.authSecretName" . }}
      key: API_READWRITE_KEY
      optional: true
- name: FACILITY_LAYOUT_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "inventory-storage.authSecretName" . }}
      key: FACILITY_LAYOUT_API_KEY
      optional: true
{{- end }}
{{- end }}
