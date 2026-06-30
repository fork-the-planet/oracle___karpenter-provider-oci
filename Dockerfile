# syntax=docker/dockerfile:1.7
#
# Karpenter Provider OCI
#
# Copyright (c) 2026 Oracle and/or its affiliates.
# Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/

# --- Builder Stage ---
ARG BUILDER_IMAGE=golang:1.26.4-alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648
ARG BASE_IMAGE=oraclelinux:8-slim@sha256:a3a035882eb662745ba8db2be2a7537932807cb4c459b71ad2a3c3dd27e8f5c0
FROM --platform=$BUILDPLATFORM $BUILDER_IMAGE AS builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY pkg/ ./pkg/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GO111MODULE=on GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -mod=mod -o /workspace/dist/operator ./cmd/main.go

FROM $BASE_IMAGE

WORKDIR /usr/local/bin/karpenter-provider-oci

COPY --from=builder /workspace/dist/operator .

USER 65532:65532

# Entrypoint
ENTRYPOINT ["/usr/local/bin/karpenter-provider-oci/operator"]
