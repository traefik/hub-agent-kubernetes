# syntax=docker/dockerfile:1.2
# Go mod
FROM --platform=$BUILDPLATFORM golang:1.17-alpine as gomod

WORKDIR /go/src/github.com/traefik/hub-agent-kubernetes

COPY go.mod .
COPY go.sum .

RUN go mod download

# Go build
FROM --platform=$BUILDPLATFORM golang:1.17-alpine as gobuild

WORKDIR /go/src/github.com/traefik/hub-agent-kubernetes

RUN apk --update upgrade \
    && apk --no-cache --no-progress add git mercurial bash gcc musl-dev curl tar ca-certificates tzdata make \
    && update-ca-certificates

COPY --from=gomod /go/pkg/ /go/pkg/
COPY . .

ARG TARGETPLATFORM
SHELL ["bash", "-c"]

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

RUN OUTPUT="dist/$TARGETPLATFORM/hub-agent-kubernetes" GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT/v/} make build

# Alpine
FROM alpine

RUN apk --no-cache --no-progress add ca-certificates tzdata git \
    && rm -rf /var/cache/apk/*

ARG TARGETPLATFORM
COPY --from=gobuild /go/src/github.com/traefik/hub-agent-kubernetes/dist/$TARGETPLATFORM/hub-agent-kubernetes /

ENTRYPOINT ["/hub-agent-kubernetes"]
EXPOSE 80
EXPOSE 443

# Metadata
LABEL org.opencontainers.image.source="https://github.com/traefik/hub-agent-kubernetes" \
    org.opencontainers.image.vendor="Traefik Labs" \
    org.opencontainers.image.url="https://traefik.io" \
    org.opencontainers.image.title="Traefik Hub" \
    org.opencontainers.image.description="The Global Networking Platform for Kubernetes" \
    org.opencontainers.image.version="$VERSION" \
    org.opencontainers.image.documentation="https://doc.traefik.io/traefik-hub"
