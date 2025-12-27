FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY notify ./notify
COPY risk ./risk

ARG SERVICE=notify
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/service ./${SERVICE}

FROM alpine:3.20
RUN adduser -D -H app
USER app

EXPOSE 8080
COPY --from=builder /out/service /service
ENTRYPOINT ["/service"]
