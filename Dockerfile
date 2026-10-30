# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS builder
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
FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

# renovate: datasource=repology depName=alpine_3_24/busybox versioning=apk syncWith=alpine
ARG BUSYBOX_VERSION=1.37.0-r31
# renovate: datasource=repology depName=alpine_3_24/libretls versioning=apk syncWith=alpine
ARG LIBRETLS_VERSION=3.8.1-r0
# renovate: datasource=repology depName=alpine_3_24/openssl versioning=apk syncWith=alpine
ARG OPENSSL_VERSION=3.5.9-r0
# renovate: datasource=repology depName=alpine_3_24/zlib versioning=apk syncWith=alpine
ARG ZLIB_VERSION=1.3.2-r1

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
