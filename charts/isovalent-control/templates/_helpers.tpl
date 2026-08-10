{{- define "ic.name" -}}
{{- .Chart.Name -}}
{{- end -}}

{{- define "ic.fullname" -}}
{{- printf "%s" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "ic.labels" -}}
app.kubernetes.io/name: {{ include "ic.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "ic.serviceAccountName" -}}
{{- default (printf "%s-backend" (include "ic.fullname" .)) .Values.serviceAccount.name -}}
{{- end -}}
