BOTTLEROCKET_SDK_VERSION = 0.15.0
BOTTLEROCKET_SDK_SITE    = cache.bottlerocket.aws
LOCAL_ARCH               = $(shell uname -m)
TARGET_ARCH              = x86_64

# the docker image that will be used to compile rust code
BUILDER_IMAGE = bottlerocket/sdk-${TARGET_ARCH}:v${BOTTLEROCKET_SDK_VERSION}-${LOCAL_ARCH}

# the target triple that will be passed to the cargo build command with as --target
RUST_TARGET = ${TARGET_ARCH}-bottlerocket-linux-musl

.PHONY: image # creates a docker image with the updater binary
image: fetch-sdk
	DOCKER_BUILDKIT=1 \
	docker build \
		-t bottlerocket-ecs-updater:latest \
		--build-arg BUILDER_IMAGE=${BUILDER_IMAGE} \
		--build-arg RUST_TARGET=${RUST_TARGET} \
		"${PWD}/updater"

.PHONY: fetch-sdk
fetch-sdk: # fetches and loads the image we use to build the updater docker image
	scripts/load-bottlerocket-sdk.sh --site ${BOTTLEROCKET_SDK_SITE} --image ${BUILDER_IMAGE}

.PHONY: check-licenses
check-licenses:
	cd updater && cargo deny check licenses
	cd integ && cargo deny check licenses

.PHONY: unit-tests
unit-tests:
	cd updater && cargo test --locked
	cd integ && cargo test --locked

.PHONY: build
build:
	cd updater && cargo build --locked
	cd integ && cargo build --locked

.PHONY: lint
lint:
	cd updater && cargo fmt -- --check
	cd updater && cargo clippy --locked -- -D warnings
	cd integ && cargo fmt -- --check
	cd integ && cargo clippy --locked -- -D warnings

.PHONY: ci # these are all of the checks (except for image) that we run for ci
ci: check-licenses lint build unit-tests
