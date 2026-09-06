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
{{/* This label is what the console's self-protection exemption keys off: every
     TracingPolicy authored in the console gets a matching NotIn expression, so
     a cluster-wide enforcing policy cannot kill the console that would have let
     you undo it. Do not rename it without changing internal/guard. */}}
app.kubernetes.io/part-of: isovalent-control
{{- end -}}

{{- define "ic.serviceAccountName" -}}
{{- default (printf "%s-backend" (include "ic.fullname" .)) .Values.serviceAccount.name -}}
{{- end -}}
