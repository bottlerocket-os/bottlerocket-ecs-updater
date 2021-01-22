#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<- EOF
    $0 [OPTIONS]

    REQUIRED ARGUMENTS:
        --site  SITE    The domain from which the Bottlerocket SDK will be fetched,
                        e.g. cache.bottlerocket.aws
        --image IMAGE   The Bottlerocket SDK image that will be fetched and loaded,
                        e.g. bottlerocket/sdk-x86_64:v0.15.0-x86_64
EOF
}

while [ ${#} -ge 1 ]; do
    case "${1}" in
        --site)
            SITE="${2}"
            shift
            ;;
        --image)
            IMAGE="${2}"
            shift
            ;;
        -?*)
            echo "ERROR: Unknown argument: ${1}"
            usage
            exit 1
            ;;
    esac
    shift
done

if [ -z "${SITE}" ]; then
  usage
  exit 1
fi

if [ -z "${IMAGE}" ]; then
  usage
  exit 1
fi

if docker image inspect "${IMAGE}" &>/dev/null; then
    echo "bottlerocket sdk image '${IMAGE}' is already loaded" >&2
    exit 0
fi

echo "fetching bottlerocket sdk image '${IMAGE}'"

if ! curl -sSL "https://${SITE}/${IMAGE}.tar.gz" | docker load;
then
    echo "failed to load bottlerocket sdk image '${IMAGE}'" >&2
    exit 1
fi
