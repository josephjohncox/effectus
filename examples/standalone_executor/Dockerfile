# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
FROM golang:1.26.8-alpine@sha256:ce864e7223ac17b1775e6fd0b4c0db580c2eb50e7953a427916379e4b92a1628 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -buildid=" \
  -o /out/business-executor ./examples/standalone_executor/executor

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/business-executor /usr/local/bin/business-executor
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/business-executor"]
