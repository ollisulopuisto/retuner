# Retuner container image for MikroTik RouterOS (RB5009 = linux/arm64).
# Retuner is compiled from this repository's source on an x86_64 build host
# (cross-compiled for arm64), so no ARM emulation is needed. The runtime
# libraries are fetched as .deb files and unpacked, so the final stage
# contains no RUN steps either.
#
# Build from the repository root: docker buildx build -f mikrotik/Dockerfile .

ARG FPC_VERSION=3.2.2
ARG INDY_COMMIT=bd38a62c0089e5e40eeb00569a09b8a8410b8d7d

FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS toolchain
ARG FPC_VERSION
ARG INDY_COMMIT
RUN dpkg --add-architecture arm64 && apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      fp-compiler fp-units-fcl fp-units-net fp-units-db fp-units-misc fp-utils lazarus-src \
      binutils-aarch64-linux-gnu libc6-dev-arm64-cross libc6-dev libsqlite3-dev libsqlite3-dev:arm64 \
      curl ca-certificates git make zip
# Debian ships no aarch64 cross compiler, so build one from the same FPC release
# as the native compiler. The 3.2.2 release passes __libc_csu_init/fini to
# __libc_start_main, which glibc 2.34 removed, so nothing it links would start.
# Debian's glibc2.34.patch passes zeros instead; its aarch64 hunk is applied here.
RUN set -e; \
    TAG=release_$(echo "$FPC_VERSION" | tr . _); \
    curl -fsSL "https://gitlab.com/freepascal.org/fpc/source/-/archive/$TAG/source-$TAG.tar.gz" | tar xz -C /tmp; \
    cd "/tmp/source-$TAG"; \
    sed -i -e '/adrp.*__libc_csu_/d' -e 's/ldr\s*\(x[34]\),.*__libc_csu_.*/mov\t\1, #0/' rtl/linux/aarch64/cprt0.as; \
    ! grep -q __libc_csu_ rtl/linux/aarch64/cprt0.as; \
    make crossall crossinstall CPU_TARGET=aarch64 OS_TARGET=linux BINUTILSPREFIX=aarch64-linux-gnu- \
      PP=ppcx64 INSTALL_PREFIX=/opt/fpc-cross; \
    ln -s "/opt/fpc-cross/lib/fpc/$FPC_VERSION/ppcrossa64" /usr/bin/ppcrossa64; \
    rm -rf "/tmp/source-$TAG"
RUN git init -q /opt/indy && cd /opt/indy && \
    git fetch -q --depth 1 https://github.com/IndySockets/Indy.git "$INDY_COMMIT" && \
    git checkout -q FETCH_HEAD

FROM --platform=$BUILDPLATFORM toolchain AS build
ARG FPC_VERSION
ARG TARGETARCH
COPY src /build/src
COPY res /build/res
COPY script/build.sh script/zip-sql.sh /build/script/
RUN set -e; \
    case "$TARGETARCH" in \
      arm64) export FPC_FLAGS="-Paarch64 -XPaarch64-linux-gnu- \
               -Fu/opt/fpc-cross/lib/fpc/$FPC_VERSION/units/aarch64-linux/* \
               -Fl/usr/aarch64-linux-gnu/lib -Fl/usr/lib/aarch64-linux-gnu -FL/lib/ld-linux-aarch64.so.1" ;; \
      amd64) ;; \
      *) echo "unsupported arch $TARGETARCH"; exit 1 ;; \
    esac; \
    INDY_DIR=/opt/indy/Lib /build/script/build.sh; \
    cp /build/bin/*/retuner /build/retuner

FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS fetch
ARG TARGETARCH
RUN set -e; \
    case "$TARGETARCH" in \
      arm64) TRIPLET=aarch64-linux-gnu ;; \
      amd64) TRIPLET=x86_64-linux-gnu ;; \
      *) echo "unsupported arch $TARGETARCH"; exit 1 ;; \
    esac; \
    dpkg --add-architecture "$TARGETARCH"; apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates; \
    mkdir -p /rootfs /tmp/deb; cd /tmp/deb; \
    apt-get download "libsqlite3-0:$TARGETARCH" "libssl3:$TARGETARCH"; \
    for f in *.deb; do dpkg-deb -x "$f" /rootfs; done; \
    # FPC's OpenSSL loader looks for the unversioned names first.
    ln -s libssl.so.3 "/rootfs/usr/lib/$TRIPLET/libssl.so"; \
    ln -s libcrypto.so.3 "/rootfs/usr/lib/$TRIPLET/libcrypto.so"; \
    mkdir -p /rootfs/etc/ssl/certs; cp /etc/ssl/certs/ca-certificates.crt /rootfs/etc/ssl/certs/
COPY cfg /tmp/cfg
# Keep all persistent state under /data (the RouterOS mount). DNS interception
# is done by RouterOS static DNS entries, so Retuner's own DNS service is off.
RUN set -e; \
    mkdir -p /rootfs/opt/retuner/defaults/config; \
    cp /tmp/cfg/avr.ini /tmp/cfg/avr.ini.example /tmp/cfg/stations.ini /tmp/cfg/stations.yaml \
       /tmp/cfg/podcasts.ini /rootfs/opt/retuner/defaults/config/; \
    sed -e 's|^CacheFolderLocation=.*|CacheFolderLocation=/data|' \
        -e 's|^ConfigFolderLocation=.*|ConfigFolderLocation=/data|' \
        -e 's|^DBFolderLocation=.*|DBFolderLocation=/data|' \
        -e 's|^MessageInfoLevel=.*|MessageInfoLevel=1|' \
        -e '/^\[DNSServer\]/,/^\[/ s|^Enable=1|Enable=0|' \
        /tmp/cfg/retuner.ini > /rootfs/opt/retuner/defaults/retuner.ini

FROM debian:bookworm-slim
COPY --from=fetch /rootfs/ /
COPY --from=build /build/retuner /opt/retuner/retuner
COPY --chmod=755 mikrotik/entrypoint.sh /entrypoint.sh
EXPOSE 80/tcp
VOLUME /data
ENTRYPOINT ["/entrypoint.sh"]
