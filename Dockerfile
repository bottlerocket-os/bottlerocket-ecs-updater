# syntax=docker/dockerfile:1.1.3-experimental
ARG BUILDER_IMAGE
# LICENSES_IMAGE is a container image that contains license files for the source
# and its dependencies. When building with `make container`, the licenses
# container image is built and provided as LICENSE_IMAGE.
ARG LICENSES_IMAGE=scratch

# build the updater image
FROM ${BUILDER_IMAGE} as builder
USER builder
WORKDIR /wrkdir
ENV GOPROXY=direct
# Sets the target architecture for the binary
ARG GOARCH
ENV OUTPUT_DIR=/wrkdir/target/${GOARCH}/release
COPY updater/go.mod updater/go.sum /wrkdir/
RUN go mod download
COPY ./updater /wrkdir/
RUN CGO_ENABLED=0 go build -v -o ${OUTPUT_DIR}/bottlerocket-ecs-updater . && \
    cp ${OUTPUT_DIR}/bottlerocket-ecs-updater /wrkdir/bottlerocket-ecs-updater

FROM ${LICENSES_IMAGE} as licenses
# Set WORKDIR to create /licenses/ if the directory is missing.
#
# Having an image with /licenses/ lets scratch be substituted in when
# LICENSES_IMAGE isn't provided. For example, a user can manually run `docker
# build -t neio:latest .` to build a working image without providing an expected
# LICENSES_IMAGE.
WORKDIR /licenses/

# create an image with just the binary
FROM scratch
# Copy CA certificates store
COPY --from=public.ecr.aws/amazonlinux/amazonlinux:2 /etc/ssl /etc/ssl
COPY --from=public.ecr.aws/amazonlinux/amazonlinux:2 /etc/pki /etc/pki
COPY --from=builder \
    /wrkdir/bottlerocket-ecs-updater \
    /bottlerocket-ecs-updater
COPY COPYRIGHT LICENSE-* /usr/share/licenses/bottlerocket-ecs-updater/
COPY --from=licenses /licenses/ /usr/share/licenses/bottlerocket-ecs-updater/vendor/
ENTRYPOINT ["/bottlerocket-ecs-updater"]
