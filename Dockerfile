# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine3.22@sha256:727cfc3c40be55cd1bc9a4a059406b28a059857e3be752aa9d09531e12c20c56 AS builder
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY api/ api/
COPY go.mod go.mod
COPY go.sum go.sum

# Copy the go source
COPY main.go main.go
#COPY api/ api/
COPY controllers/ controllers/
COPY manager/ manager/

# Build
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" GO111MODULE=on \
    go build -a -o converter main.go

# Use alpine as minimal base image to package the converter binary
FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

# renovate: datasource=repology depName=alpine_3_24/busybox versioning=apk syncWith=alpine
ARG BUSYBOX_VERSION=1.37.0-r31
# renovate: datasource=repology depName=alpine_3_24/libretls versioning=apk syncWith=alpine
ARG LIBRETLS_VERSION=3.8.1-r0
# renovate: datasource=repology depName=alpine_3_24/openssl versioning=apk syncWith=alpine
ARG OPENSSL_VERSION=3.5.7-r0
# renovate: datasource=repology depName=alpine_3_24/zlib versioning=apk syncWith=alpine
ARG ZLIB_VERSION=1.3.2-r0

RUN apk add --no-cache --upgrade \
        busybox="${BUSYBOX_VERSION}" \
        libretls="${LIBRETLS_VERSION}" \
        openssl="${OPENSSL_VERSION}" \
        zlib="${ZLIB_VERSION}"

ENV USER_UID=2001 \
    USER_NAME=converter \
    GROUP_NAME=converter

WORKDIR /
COPY --from=builder --chown=${USER_UID} /workspace/converter .

RUN addgroup ${GROUP_NAME} && adduser -D -G ${GROUP_NAME} -u ${USER_UID} ${USER_NAME}
USER ${USER_UID}

ENTRYPOINT ["/converter"]
