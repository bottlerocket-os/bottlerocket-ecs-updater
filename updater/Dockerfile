# syntax=docker/dockerfile:1.1.3-experimental
FROM clux/muslrust as builder
COPY . /volume
RUN cargo build --locked --release --target x86_64-unknown-linux-musl

FROM alpine
COPY --from=builder \
    /volume/target/x86_64-unknown-linux-musl/release/bottlerocket-ecs-updater \
    /ecs-updater/bin/
WORKDIR /ecs-updater
