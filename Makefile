UPDATER_IMAGE=bottlerocket-update-operator:latest

.PHONY: image
image: ## builds the updater docker image
	docker build -t ${UPDATER_IMAGE} "${PWD}/updater"

.PHONY: ci
ci: ci-checks image ## the checks that we run for pull requests.

.PHONY: ci-checks
ci-checks: ## checks fmt, clippy, build and unit tests for the two rust projects.
	cd updater && cargo fmt -- --check
	cd updater cargo clippy --locked -- -D warnings
	cd updater && cargo build --locked
	cd updater && cargo test --locked
	cd updater && cargo deny check licenses
	cd integ && cargo fmt -- --check
	cd integ cargo clippy --locked -- -D warnings
	cd integ && cargo build --locked
	cd integ && cargo test --locked
	cd integ && cargo deny check licenses
