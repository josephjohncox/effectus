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
      app.kubernetes.io/component: server
  template:
    metadata:
      labels:
        {{- include "effectusd.selectorLabels" . | nindent 8 }}
        app.kubernetes.io/component: server
      annotations:
        effectus.dev/rollout-nonce: {{ .Values.rolloutNonce | quote }}
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
          {{- else if .Values.image.unsafeAllowTag }}
          image: "{{ .Values.image.repository }}:{{ required "image.tag is required when image.unsafeAllowTag is true" .Values.image.tag }}"
          {{- else }}
          {{- fail "image.digest is required; mutable image tags require image.unsafeAllowTag=true" }}
          {{- end }}
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          securityContext:
            {{- toYaml .Values.containerSecurityContext | nindent 12 }}
          args:
            - "--oci-ref={{ required "bundle.ociRef is required" .Values.bundle.ociRef }}"
            - "--oci-signature-verifier={{ required "bundle.signatureVerifier is required for OCI loading" .Values.bundle.signatureVerifier }}"
            - "--http-addr=:{{ .Values.service.port }}"
            - "--database-migrations=validate"
            {{- if .Values.grpc.enabled }}
            - "--grpc-addr=:{{ .Values.grpc.port }}"
            - "--grpc-tls-cert=/etc/effectus-grpc-tls/{{ .Values.grpc.certKey }}"
            - "--grpc-tls-key=/etc/effectus-grpc-tls/{{ .Values.grpc.keyKey }}"
            {{- end }}
          env:
            - name: EFFECTUS_POSTGRES_DSN
              valueFrom:
                secretKeyRef:
                  name: {{ required "postgres.existingSecret is required" .Values.postgres.existingSecret }}
                  key: {{ .Values.postgres.dsnKey }}
            - name: EFFECTUS_API_TOKEN
              valueFrom:
                secretKeyRef:
                  name: {{ required "api.existingSecret is required" .Values.api.existingSecret }}
                  key: {{ .Values.api.tokenKey }}
          ports:
            - name: http
              containerPort: {{ .Values.service.port }}
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
            {{- if .Values.grpc.enabled }}
            - name: grpc-tls
              mountPath: /etc/effectus-grpc-tls
              readOnly: true
            {{- end }}
            {{- with .Values.extraVolumeMounts }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
      volumes:
        {{- if .Values.grpc.enabled }}
        - name: grpc-tls
          secret:
            secretName: {{ required "grpc.existingTLSSecret is required when gRPC is enabled" .Values.grpc.existingTLSSecret }}
        {{- end }}
        {{- with .Values.extraVolumes }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
