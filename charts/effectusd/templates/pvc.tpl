{{- if and .Values.persistence.enabled (not .Values.persistence.existingClaim) }}
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: {{ include "effectusd.fullname" . }}-data
  labels:
    {{- include "effectusd.labels" . | nindent 4 }}
spec:
  accessModes:
    - ReadWriteOnce
  {{- if .Values.persistence.storageClass }}
  storageClassName: {{ .Values.persistence.storageClass }}
  {{- end }}
  resources:
    requests:
      storage: {{ .Values.persistence.size }}
{{- end }}
