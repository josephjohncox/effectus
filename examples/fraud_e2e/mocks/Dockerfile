FROM golang:1.25.13-alpine@sha256:1e0126852075c9c60731c8ba49088448b91f63e2aed97ca9d1a9791622a05946 AS builder

WORKDIR /src
COPY go.mod ./
COPY notify ./notify
COPY risk ./risk

ARG SERVICE=notify
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/service ./${SERVICE}

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce
RUN apk upgrade --no-cache && adduser -D -H app
USER app

EXPOSE 8080
COPY --from=builder /out/service /service
ENTRYPOINT ["/service"]
