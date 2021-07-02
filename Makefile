# SHELL is set as bash to use some bashisms.
SHELL = bash

BOTTLEROCKET_SDK_VERSION = v0.22.0
BOTTLEROCKET_SDK_ARCH    = x86_64
UPDATER_TARGET_ARCH      = amd64

# the docker image that will be used to compile go code
BUILDER_IMAGE = public.ecr.aws/bottlerocket/bottlerocket-sdk-${BOTTLEROCKET_SDK_ARCH}:${BOTTLEROCKET_SDK_VERSION}

# IMAGE_NAME is the full name of the container being built
IMAGE_NAME = bottlerocket-ecs-updater:latest
# LICENSES_IMAGE is the name of the container image that has LICENSE files
# for distribution.
LICENSES_IMAGE = $(IMAGE_NAME)-licenses

SOURCEDIR=./updater
SOURCES := $(shell find $(SOURCEDIR) -name '*.go')
export GO111MODULE=on
export DOCKER_BUILDKIT=1

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
image: licenses
	docker build \
		--tag '$(IMAGE_NAME)' \
		--build-arg BUILDER_IMAGE=$(BUILDER_IMAGE) \
		--build-arg GOARCH=$(UPDATER_TARGET_ARCH) \
		--build-arg LICENSES_IMAGE=$(LICENSES_IMAGE) \
		.

.PHONY: licenses
licenses:
	docker build \
		--tag '$(LICENSES_IMAGE)' \
		--build-arg SDK_IMAGE=$(BUILDER_IMAGE) \
		--build-arg GOLANG_IMAGE=$(BUILDER_IMAGE) \
		--build-arg GOARCH=$(UPDATER_TARGET_ARCH) \
		-f Dockerfile.licenses \
		.

.PHONY: lint
lint: golang-lint cfn-lint

.PHONY: golang-lint
golang-lint:
	cd updater; golangci-lint run

.PHONY: cfn-lint
cfn-lint:
	cfn-lint ./stacks/bottlerocket-ecs-updater.yaml
	cfn-lint ./integ/stacks/integ-shared.yaml
	cfn-lint ./integ/stacks/cluster.yaml

# Check that the container has LICENSE files included for its dependencies.
.PHONY: check-licenses
check-licenses: CHECK_CONTAINER_NAME=check-licenses-bottlerocket-ecs-updater
check-licenses:
	@echo "Running check: $@"
	@-if docker inspect $(CHECK_CONTAINER_NAME) &>/dev/null; then\
		docker rm $(CHECK_CONTAINER_NAME) &>/dev/null; \
	fi
	@docker create --name $(CHECK_CONTAINER_NAME) $(IMAGE_NAME) >/dev/null 2>&1
	@echo "Checking if container image included dependencies' LICENSE files..."
	@docker export $(CHECK_CONTAINER_NAME) | tar -tf - \
		| grep usr/share/licenses/bottlerocket-ecs-updater/vendor \
		| grep -q LICENSE || { \
			echo "Container image is missing required LICENSE files (checked $(IMAGE_NAME))"; \
			docker rm $(CHECK_CONTAINER_NAME) &>/dev/null; \
			exit 1; \
		}
	@-docker rm $(CHECK_CONTAINER_NAME)

clean:
	-rm -rf updater/bin
