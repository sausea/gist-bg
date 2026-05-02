#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARCH="${1:-amd64}"
VERSION="$(sed -n 's/.*AppVersion = "\(.*\)"/\1/p' "${ROOT_DIR}/backend/internal/config/config.go" | head -n 1)"
case "$ARCH" in
  amd64|arm64) ;;
  *)
    echo "unsupported arch: $ARCH (expected amd64 or arm64)" >&2
    exit 1
    ;;
esac

PLATFORM="linux/${ARCH}"
if [[ -z "${VERSION}" ]]; then
  echo "failed to detect AppVersion from backend/internal/config/config.go" >&2
  exit 1
fi

IMAGE_TAG="gist:offline-${ARCH}"
VERSIONED_IMAGE_TAG="gist:${VERSION}-offline-${ARCH}"
OUTPUT_DIR="${ROOT_DIR}/dist/offline"
ARCHIVE_NAME="gist-v${VERSION}-offline_linux-${ARCH}.tar.gz"
ARCHIVE_PATH="${OUTPUT_DIR}/${ARCHIVE_NAME}"
CHECKSUM_PATH="${ARCHIVE_PATH}.sha256"

mkdir -p "${OUTPUT_DIR}"

echo "==> Building ${IMAGE_TAG} and ${VERSIONED_IMAGE_TAG} for ${PLATFORM}"
docker buildx build \
  --platform "${PLATFORM}" \
  --load \
  -f "${ROOT_DIR}/docker/Dockerfile" \
  -t "${IMAGE_TAG}" \
  -t "${VERSIONED_IMAGE_TAG}" \
  "${ROOT_DIR}"

echo "==> Saving ${IMAGE_TAG} to ${ARCHIVE_PATH}"
docker save "${IMAGE_TAG}" "${VERSIONED_IMAGE_TAG}" | gzip -9 > "${ARCHIVE_PATH}"

echo "==> Writing checksum ${CHECKSUM_PATH}"
if command -v sha256sum >/dev/null 2>&1; then
  LC_ALL=C sha256sum "${ARCHIVE_PATH}" > "${CHECKSUM_PATH}"
elif command -v shasum >/dev/null 2>&1; then
  LC_ALL=C shasum -a 256 "${ARCHIVE_PATH}" > "${CHECKSUM_PATH}"
elif command -v openssl >/dev/null 2>&1; then
  LC_ALL=C openssl dgst -sha256 "${ARCHIVE_PATH}" | awk -v file="${ARCHIVE_PATH}" '{print $NF "  " file}' > "${CHECKSUM_PATH}"
else
  echo "no checksum tool found (need sha256sum, shasum, or openssl)" >&2
  exit 1
fi

cat <<EOF

Offline bundle created:
  Image:    ${IMAGE_TAG}
  Version:  ${VERSIONED_IMAGE_TAG}
  Archive:  ${ARCHIVE_PATH}
  Checksum: ${CHECKSUM_PATH}

Import on the offline machine with:
  gunzip -c ${ARCHIVE_NAME} | docker load

EOF
