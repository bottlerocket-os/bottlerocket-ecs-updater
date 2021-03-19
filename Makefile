BOTTLEROCKET_SDK_VERSION = 0.15.0
BOTTLEROCKET_SDK_ARCH    = x86_64
UPDATER_TARGET_ARCH      = amd64

# the docker image that will be used to compile go code
BUILDER_IMAGE = public.ecr.aws/bottlerocket/bottlerocket-sdk-${BOTTLEROCKET_SDK_ARCH}:${BOTTLEROCKET_SDK_VERSION}

.PHONY: fetch-dependency # downloads go.mod dependency
fetch-dependency:
	cd updater && GO111MODULE=on go mod tidy

.PHONY: build # builds updater
build: fetch-dependency
	GOARCH=$(UPDATER_TARGET_ARCH)
	cd updater && GO111MODULE=on go build -v -o bin/bottlerocket-ecs-updater .

.PHONY: image # creates a docker image with the updater binary
image: build
	DOCKER_BUILDKIT=1 \
	docker build \
		-t bottlerocket-ecs-updater:latest \
		--build-arg BUILDER_IMAGE=${BUILDER_IMAGE} \
		--build-arg GOARCH=${UPDATER_TARGET_ARCH} \
		"${PWD}/updater"
