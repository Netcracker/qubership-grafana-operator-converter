# Build the manager binary
FROM golang:1.24.3-alpine3.20 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY api/ api/
COPY go.mod go.mod
COPY go.sum go.sum

# Copy the go source
COPY main.go main.go
#COPY api/ api/
COPY controllers/ controllers/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o converter main.go

# Use alpine as minimal base image to package the converter binary
FROM alpine:3.23.3

RUN apk add --upgrade \
        busybox \
        libretls \
        openssl \
        zlib \
    && rm -rf /var/cache/apk/*

ENV USER_UID=2001 \
    USER_NAME=converter \
    GROUP_NAME=converter

WORKDIR /
COPY --from=builder --chown=${USER_UID} /workspace/converter .

RUN addgroup ${GROUP_NAME} && adduser -D -G ${GROUP_NAME} -u ${USER_UID} ${USER_NAME}
USER ${USER_UID}

ENTRYPOINT ["/converter"]
