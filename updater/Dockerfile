# syntax=docker/dockerfile:1.1.3-experimental
ARG BUILDER_IMAGE

# a layer to speed up rebuilds
FROM scratch AS buildcache
USER 1000:1000
WORKDIR /targetdir

# a layer to cache the contents of cargo home
FROM scratch AS cargohome
USER 1000:1000
WORKDIR /cargo_home

# build the updater image
FROM ${BUILDER_IMAGE} as builder
ARG RUST_TARGET

USER root
USER builder
WORKDIR /wrkdir
ENV CARGO_HOME=/cargo_home
ENV OUTPUT_DIR=/wrkdir/target/${RUST_TARGET}/release
COPY ./Cargo.toml ./Cargo.lock ./
COPY ./src ./src
# mount cache volumes for cargo targets and cargo home, build the updater
# binary, and copy the binary out of the cache volume
RUN --mount=type=cache,target=/wrkdir/target,from=buildcache,source=/targetdir \
    --mount=type=cache,target=/cargo_home,from=cargohome,source=/cargo_home \
    cargo build --locked --release --target ${RUST_TARGET} && \
    cp ${OUTPUT_DIR}/bottlerocket-ecs-updater /wrkdir/bottlerocket-ecs-updater

# create an image with just the binary
FROM scratch
# rusttls requires CA certificates store
COPY --from=amazonlinux:2 /etc/ssl /etc/ssl
COPY --from=amazonlinux:2 /etc/pki /etc/pki
COPY --from=builder \
    /wrkdir/bottlerocket-ecs-updater \
    /usr/local/bin/bottlerocket-ecs-updater
