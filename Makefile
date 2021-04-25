BOTTLEROCKET_SDK_VERSION = 0.15.0
BOTTLEROCKET_SDK_ARCH    = x86_64
UPDATER_TARGET_ARCH      = amd64

# the docker image that will be used to compile go code
BUILDER_IMAGE = public.ecr.aws/bottlerocket/bottlerocket-sdk-${BOTTLEROCKET_SDK_ARCH}:${BOTTLEROCKET_SDK_VERSION}
SOURCEDIR=./updater
SOURCES := $(shell find $(SOURCEDIR) -name '*.go')
export GO111MODULE=on

all: build

.PHONY: tidy
tidy:
	cd updater && go mod tidy

.PHONY: build # builds updater
build: updater/bin/bottlerocket-ecs-updater
updater/bin/bottlerocket-ecs-updater: $(SOURCES) updater/go.mod updater/go.sum
	GOARCH=$(UPDATER_TARGET_ARCH)
	cd updater && go build -v -o bin/bottlerocket-ecs-updater .

.PHONY: test
test:
	cd updater && go test -v ./...


.PHONY: image # creates a docker image with the updater binary
image:
	DOCKER_BUILDKIT=1 \
	docker build \
		-t bottlerocket-ecs-updater:latest \
		--build-arg BUILDER_IMAGE=${BUILDER_IMAGE} \
		--build-arg GOARCH=${UPDATER_TARGET_ARCH} \
		"${PWD}/updater"

clean:
	-rm -rf updater/bin
