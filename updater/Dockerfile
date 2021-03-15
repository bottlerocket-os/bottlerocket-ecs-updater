# syntax=docker/dockerfile:1.1.3-experimental
ARG BUILDER_IMAGE

# build the updater image
FROM ${BUILDER_IMAGE} as builder
USER builder
WORKDIR /wrkdir
ENV GOPROXY=direct
# Sets the target architecture for the binary
ARG GOARCH
ENV OUTPUT_DIR=/wrkdir/target/${GOARCH}/release
COPY go.mod go.sum /wrkdir/
RUN go mod download
COPY ./ /wrkdir/
RUN CGO_ENABLED=0 go build -v -o ${OUTPUT_DIR}/bottlerocket-ecs-updater . && \
    cp ${OUTPUT_DIR}/bottlerocket-ecs-updater /wrkdir/bottlerocket-ecs-updater

# create an image with just the binary
FROM scratch
# Copy CA certificates store
COPY --from=public.ecr.aws/amazonlinux/amazonlinux:2 /etc/ssl /etc/ssl
COPY --from=public.ecr.aws/amazonlinux/amazonlinux:2 /etc/pki /etc/pki
COPY --from=builder \
    /wrkdir/bottlerocket-ecs-updater \
    /bottlerocket-ecs-updater
ENTRYPOINT ["/bottlerocket-ecs-updater"]
