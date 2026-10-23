ARG BUILD_FROM=ghcr.io/home-assistant/amd64-base-debian:bookworm

# The add-on's build context is this directory, not the repository root, so the
# source is fetched rather than copied. Bump RETUNER_REF in build.yaml to move
# the add-on to a different revision.
FROM ${BUILD_FROM} AS builder

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates git zip \
      golang-go \
      libc6-dev libsqlite3-dev \
 && rm -rf /var/lib/apt/lists/*

ARG RETUNER_REPO=https://github.com/ollisulopuisto/retuner.git
ARG RETUNER_REF=master
RUN git clone --depth 1 --branch "${RETUNER_REF}" "${RETUNER_REPO}" /src

WORKDIR /src
RUN bash ./script/build.sh

FROM ${BUILD_FROM}

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates libsqlite3-0 libssl3 \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/bin/*/retuner /opt/retuner/retuner
COPY --from=builder /src/cfg/ /opt/retuner/cfg/
COPY rootfs /

LABEL \
    org.opencontainers.image.title="Home Assistant Add-on: Retuner" \
    org.opencontainers.image.description="Internet radio for AV receivers whose vTuner service was shut down" \
    org.opencontainers.image.source="https://github.com/ollisulopuisto/retuner" \
    org.opencontainers.image.licenses="MIT"
