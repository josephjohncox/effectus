apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "effectusd.fullname" . }}
  labels:
    {{- include "effectusd.labels" . | nindent 4 }}
spec:
  replicas: 1
  selector:
    matchLabels:
      {{- include "effectusd.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "effectusd.selectorLabels" . | nindent 8 }}
      {{- if .Values.config.enabled }}
      annotations:
        checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
      {{- end }}
    spec:
      terminationGracePeriodSeconds: {{ .Values.terminationGracePeriodSeconds }}
      securityContext:
        {{- toYaml .Values.podSecurityContext | nindent 8 }}
      {{- with .Values.initContainers }}
      initContainers:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
        - name: effectusd
          {{- if .Values.image.digest }}
          image: "{{ .Values.image.repository }}@{{ .Values.image.digest }}"
          {{- else }}
          image: "{{ .Values.image.repository }}:{{ required "image.tag or image.digest is required" .Values.image.tag }}"
          {{- end }}
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          securityContext:
            {{- toYaml .Values.containerSecurityContext | nindent 12 }}
          args:
            {{- if .Values.config.enabled }}
            - "--config={{ .Values.config.mountPath }}/{{ .Values.config.key }}"
            {{- else }}
            - "--http-addr=:{{ .Values.service.port }}"
            - "--metrics-addr=:{{ .Values.service.metricsPort }}"
            - "--oci-ref={{ required "bundle.ociRef is required when config.enabled is false" .Values.bundle.ociRef }}"
            - "--oci-cache-dir={{ .Values.bundle.cacheDir }}"
            {{- if .Values.bundle.reloadInterval }}
            - "--reload-interval={{ .Values.bundle.reloadInterval }}"
            {{- end }}
            - "--facts-store={{ .Values.facts.store }}"
            - "--facts-path={{ .Values.facts.path }}"
            - "--facts-merge-default={{ .Values.facts.mergeDefault }}"
            {{- range .Values.facts.mergeNamespace }}
            - "--facts-merge-namespace={{ . }}"
            {{- end }}
            - "--facts-cache-policy={{ .Values.facts.cachePolicy }}"
            - "--facts-cache-max-universes={{ .Values.facts.cacheMaxUniverses }}"
            - "--facts-cache-max-namespaces={{ .Values.facts.cacheMaxNamespaces }}"
            {{- end }}
            - "--api-auth={{ .Values.api.authMode }}"
            - "--api-rate-limit={{ .Values.api.rateLimit }}"
            - "--api-rate-burst={{ .Values.api.rateBurst }}"
            {{- if eq .Values.api.authMode "token" }}
            - "--api-token=$(EFFECTUS_API_TOKEN)"
            - "--api-read-token=$(EFFECTUS_API_READ_TOKEN)"
            {{- end }}
            {{- if .Values.api.aclFile }}
            - "--api-acl-file={{ .Values.api.aclFile }}"
            {{- end }}
          {{- if eq .Values.api.authMode "token" }}
          env:
            - name: EFFECTUS_API_TOKEN
              valueFrom:
                secretKeyRef:
                  name: {{ required "api.existingSecret is required for token authentication" .Values.api.existingSecret }}
                  key: {{ .Values.api.tokenKey }}
            - name: EFFECTUS_API_READ_TOKEN
              valueFrom:
                secretKeyRef:
                  name: {{ required "api.existingSecret is required for token authentication" .Values.api.existingSecret }}
                  key: {{ .Values.api.readTokenKey }}
                  optional: true
          {{- end }}
          ports:
            - name: http
              containerPort: {{ .Values.service.port }}
            - name: metrics
              containerPort: {{ .Values.service.metricsPort }}
          startupProbe:
            httpGet:
              path: /healthz
              port: http
            failureThreshold: 30
            periodSeconds: 2
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
            periodSeconds: 5
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            periodSeconds: 10
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          volumeMounts:
            - name: data
              mountPath: /data
            {{- if .Values.config.enabled }}
            - name: config
              mountPath: {{ .Values.config.mountPath }}
              readOnly: true
            {{- end }}
            {{- with .Values.extraVolumeMounts }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
      volumes:
        - name: data
          {{- if .Values.persistence.enabled }}
          persistentVolumeClaim:
            claimName: {{ default (printf "%s-data" (include "effectusd.fullname" .)) .Values.persistence.existingClaim }}
          {{- else }}
          emptyDir: {}
          {{- end }}
        {{- if .Values.config.enabled }}
        - name: config
          configMap:
            name: {{ default (printf "%s-config" (include "effectusd.fullname" .)) .Values.config.name }}
        {{- end }}
        {{- with .Values.extraVolumes }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
