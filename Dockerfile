# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder
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
FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

# renovate: datasource=repology depName=alpine_3_24/busybox versioning=apk syncWith=alpine
ARG BUSYBOX_VERSION=1.37.0-r31
# renovate: datasource=repology depName=alpine_3_24/libretls versioning=apk syncWith=alpine
ARG LIBRETLS_VERSION=3.8.1-r0
# renovate: datasource=repology depName=alpine_3_24/openssl versioning=apk syncWith=alpine
ARG OPENSSL_VERSION=3.5.9-r0
# renovate: datasource=repology depName=alpine_3_24/zlib versioning=apk syncWith=alpine
ARG ZLIB_VERSION=1.4-r0

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
