.PHONY: image
image:
	docker build -t bottlerocket-update-operator:latest "${PWD}/updater"

.PHONY: x86_64-unknown-linux-musl
x86_64-unknown-linux-musl:
	docker build -t bottlerocket-update-operator:latest "${PWD}/updater"
