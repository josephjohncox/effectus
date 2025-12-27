{{- define "effectusd.name" -}}
effectusd
{{- end }}

{{- define "effectusd.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "effectusd.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "effectusd.labels" -}}
app.kubernetes.io/name: {{ include "effectusd.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{- define "effectusd.selectorLabels" -}}
app.kubernetes.io/name: {{ include "effectusd.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
