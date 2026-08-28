{{- $runtimeConfig := (.Values.config.contents | fromYaml) | default dict -}}
{{- $runtimeBundle := (get $runtimeConfig "bundle") | default dict -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "effectusd.fullname" . }}
  labels:
    {{- include "effectusd.labels" . | nindent 4 }}
spec:
  # Checked execution identity is version-sensitive. Never overlap daemon pods.
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      {{- include "effectusd.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "effectusd.selectorLabels" . | nindent 8 }}
      annotations:
        effectus.dev/rollout-nonce: {{ .Values.rolloutNonce | quote }}
        {{- if .Values.config.enabled }}
        checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
        {{- end }}
        {{- with .Values.podAnnotations }}
        {{- toYaml . | nindent 8 }}
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
            - "--oci-ref={{ required "bundle.ociRef is required when config.enabled is false" .Values.bundle.ociRef }}"
            - "--oci-signature-verifier={{ required "bundle.signatureVerifier is required for OCI loading" .Values.bundle.signatureVerifier }}"
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
            # Chart-owned operational settings override ConfigMap values.
            - "--http-addr=:{{ .Values.service.port }}"
            - "--metrics-addr=:{{ .Values.service.metricsPort }}"
            - "--oci-cache-dir={{ required "bundle.cacheDir must be a writable mounted path" .Values.bundle.cacheDir }}"
            - "--database-migrations=validate"
            - "--database-max-open={{ .Values.postgres.pool.maxOpen }}"
            - "--database-max-idle={{ .Values.postgres.pool.maxIdle }}"
            - "--database-max-lifetime={{ .Values.postgres.pool.maxLifetime }}"
            - "--database-max-idle-time={{ .Values.postgres.pool.maxIdleTime }}"
            - "--shutdown-timeout={{ .Values.shutdownTimeout }}"
            {{- if and .Values.config.enabled .Values.bundle.signatureVerifier }}
            - "--oci-signature-verifier={{ .Values.bundle.signatureVerifier }}"
            {{- end }}
            {{- if .Values.grpc.enabled }}
            - "--grpc-addr=:{{ .Values.grpc.port }}"
            - "--grpc-tls-cert=/etc/effectus-grpc-tls/{{ .Values.grpc.certKey }}"
            - "--grpc-tls-key=/etc/effectus-grpc-tls/{{ .Values.grpc.keyKey }}"
            - "--grpc-max-receive-bytes={{ .Values.grpc.maxReceiveBytes }}"
            - "--grpc-max-send-bytes={{ .Values.grpc.maxSendBytes }}"
            - "--grpc-max-concurrent={{ .Values.grpc.maxConcurrent }}"
            {{- end }}
            - "--api-auth={{ .Values.api.authMode }}"
            - "--api-rate-limit={{ .Values.api.rateLimit }}"
            - "--api-rate-burst={{ .Values.api.rateBurst }}"
            - "--api-limiter-capacity={{ .Values.api.limiterCapacity }}"
            - "--api-limiter-idle-ttl={{ .Values.api.limiterIdleTTL }}"
            {{- if .Values.api.trustedProxyCIDRs }}
            - "--trusted-proxy-cidrs={{ .Values.api.trustedProxyCIDRs }}"
            {{- end }}
            {{- if .Values.api.aclFile }}
            - "--api-acl-file={{ .Values.api.aclFile }}"
            {{- end }}
          env:
            - name: EFFECTUS_POSTGRES_DSN
              valueFrom:
                secretKeyRef:
                  name: {{ required "postgres.existingSecret is required" .Values.postgres.existingSecret }}
                  key: {{ .Values.postgres.dsnKey }}
          {{- if eq .Values.api.authMode "token" }}
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
            {{- if .Values.grpc.enabled }}
            - name: grpc
              containerPort: {{ .Values.grpc.port }}
            {{- end }}
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
            {{- if .Values.grpc.enabled }}
            - name: grpc-tls
              mountPath: /etc/effectus-grpc-tls
              readOnly: true
            {{- end }}
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
        {{- if .Values.grpc.enabled }}
        - name: grpc-tls
          secret:
            secretName: {{ required "grpc.existingTLSSecret is required when gRPC is enabled" .Values.grpc.existingTLSSecret }}
        {{- end }}
        {{- if .Values.config.enabled }}
        - name: config
          configMap:
            name: {{ default (printf "%s-config" (include "effectusd.fullname" .)) .Values.config.name }}
        {{- end }}
        {{- with .Values.extraVolumes }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
