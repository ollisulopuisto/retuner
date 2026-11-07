# Viritin container image for MikroTik RouterOS (RB5009 = linux/arm64).
# Viritin is compiled from this repository's source on an x86_64 build host
# (cross-compiled for arm64), so no ARM emulation is needed.
#
# Build from the repository root: docker buildx build -f mikrotik/Dockerfile .

FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS build
ARG TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY internal internal/
RUN set -e; \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /build/viritin main.go

FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS fetch
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates && \
    mkdir -p /rootfs/etc/ssl/certs && \
    cp /etc/ssl/certs/ca-certificates.crt /rootfs/etc/ssl/certs/
COPY cfg /tmp/cfg
# Keep all persistent state under /data (the RouterOS mount). DNS interception
# is done by RouterOS static DNS entries, so Viritin's own DNS service is off.
RUN set -e; \
    mkdir -p /rootfs/opt/viritin/defaults/config; \
    cp /tmp/cfg/avr.ini /tmp/cfg/avr.ini.example /tmp/cfg/stations.ini /tmp/cfg/stations.yaml \
       /tmp/cfg/podcasts.ini /rootfs/opt/viritin/defaults/config/; \
    sed -e 's|^CacheFolderLocation=.*|CacheFolderLocation=/data|' \
        -e 's|^ConfigFolderLocation=.*|ConfigFolderLocation=/data|' \
        -e 's|^DBFolderLocation=.*|DBFolderLocation=/data|' \
        -e 's|^MessageInfoLevel=.*|MessageInfoLevel=1|' \
        -e '/^\[DNSServer\]/,/^\[/ s|^Enable=1|Enable=0|' \
        /tmp/cfg/viritin.ini > /rootfs/opt/viritin/defaults/viritin.ini

FROM debian:bookworm-slim
COPY --from=fetch /rootfs/ /
COPY --from=build /build/viritin /opt/viritin/viritin
COPY --chmod=755 mikrotik/entrypoint.sh /entrypoint.sh
EXPOSE 80/tcp
VOLUME /data
ENTRYPOINT ["/entrypoint.sh"]

