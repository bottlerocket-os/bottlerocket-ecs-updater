BOTTLEROCKET_SDK_VERSION = 0.15.0
BOTTLEROCKET_SDK_ARCH    = x86_64
UPDATER_TARGET_ARCH      = amd64

# the docker image that will be used to compile go code
BUILDER_IMAGE = public.ecr.aws/bottlerocket/bottlerocket-sdk-${BOTTLEROCKET_SDK_ARCH}:${BOTTLEROCKET_SDK_VERSION}

.PHONY: image # creates a docker image with the updater binary
image:
	DOCKER_BUILDKIT=1 \
	docker build \
		-t bottlerocket-ecs-updater:latest \
		--build-arg BUILDER_IMAGE=${BUILDER_IMAGE} \
		--build-arg GOARCH=${UPDATER_TARGET_ARCH} \
		"${PWD}/updater"
