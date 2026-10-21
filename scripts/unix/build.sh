#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/env.sh"

command -v go >/dev/null 2>&1 || { echo "Go is required" >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "npm is required: refusing to embed stale frontend assets" >&2; exit 1; }
[[ -f "${ROOT_DIR}/web/package-lock.json" ]] || { echo "missing web/package-lock.json" >&2; exit 1; }

VERSION="${VERSION:-dev}"
HOST_OS="$(go env GOHOSTOS)"
HOST_ARCH="$(go env GOHOSTARCH)"
TARGET_OS="${GOOS:-${HOST_OS}}"
TARGET_ARCH="${GOARCH:-${HOST_ARCH}}"
GO_BUILD_JOBS="${GO_BUILD_JOBS:-1}"
if ! [[ "${GO_BUILD_JOBS}" =~ ^[1-9][0-9]*$ ]]; then
  echo "GO_BUILD_JOBS must be >= 1" >&2
  exit 1
fi
export NODE_OPTIONS="${NODE_OPTIONS:-} --no-deprecation"
unset npm_config_devdir NPM_CONFIG_DEVDIR || true

# Keep native binary name for run.sh and start.sh; cross targets are explicit.
OUTPUT_BASE="${BIN_PATH%.exe}"
if [[ "${TARGET_OS}" == "${HOST_OS}" && "${TARGET_ARCH}" == "${HOST_ARCH}" ]]; then
  OUTPUT_BIN="${OUTPUT_BASE}"
elif [[ "${TARGET_ARCH}" == "arm" && -n "${GOARM:-}" ]]; then
  OUTPUT_BIN="${OUTPUT_BASE}-${TARGET_OS}-armv${GOARM}"
else
  OUTPUT_BIN="${OUTPUT_BASE}-${TARGET_OS}-${TARGET_ARCH}"
fi
if [[ "${TARGET_OS}" == "windows" ]]; then OUTPUT_BIN="${OUTPUT_BIN}.exe"; fi
mkdir -p "$(dirname "${OUTPUT_BIN}")"

echo "building complete ${APP_NAME} source tree"
echo "  host:   ${HOST_OS}/${HOST_ARCH}"
echo "  target: ${TARGET_OS}/${TARGET_ARCH}"
echo "  output: ${OUTPUT_BIN}"
echo "  cgo:    ${CGO_ENABLED}"

cd "${ROOT_DIR}"
echo "[1/5] synchronize frontend dependencies (npm ci)"
npm --prefix web ci --prefer-offline --no-audit --no-fund

echo "[2/5] build current frontend"
npm --prefix web run build
[[ -f "${ROOT_DIR}/web/dist/index.html" && -d "${ROOT_DIR}/web/dist/assets" ]] || {
  echo "frontend output dist/index.html or dist/assets is missing" >&2
  exit 1
}

echo "[3/5] refresh embedded frontend assets"
STATIC_DIR="${ROOT_DIR}/internal/ui/static"
mkdir -p "${STATIC_DIR}"
rm -rf "${STATIC_DIR}/assets"
cp -R "${ROOT_DIR}/web/dist/." "${STATIC_DIR}/"
[[ -f "${STATIC_DIR}/index.html" && -d "${STATIC_DIR}/assets" ]] || {
  echo "copying frontend assets into Go embed failed" >&2
  exit 1
}

echo "[4/5] compile every Go module"
for module in "." "third/lal" "third/lalmax"; do
  echo "  module: ${module}"
  ( cd "${ROOT_DIR}/${module}" &&
    CGO_ENABLED="${CGO_ENABLED}" GOOS="${TARGET_OS}" GOARCH="${TARGET_ARCH}" \
      go build -p "${GO_BUILD_JOBS}" ./... )
done

echo "[5/5] build final NVR binary"
TEMP_BIN="${OUTPUT_BIN}.build-tmp"
trap 'rm -f "${TEMP_BIN}"' EXIT
rm -f "${TEMP_BIN}"
CGO_ENABLED="${CGO_ENABLED}" GOOS="${TARGET_OS}" GOARCH="${TARGET_ARCH}" \
  go build -p "${GO_BUILD_JOBS}" \
  -ldflags "-s -w -X main.appVersion=${VERSION}" \
  -o "${TEMP_BIN}" ./cmd/lalmax-nvr
[[ -f "${TEMP_BIN}" ]] || { echo "final binary is missing" >&2; exit 1; }
go version -m "${TEMP_BIN}"
mv -f "${TEMP_BIN}" "${OUTPUT_BIN}"
trap - EXIT
echo "BUILD PASS: ${OUTPUT_BIN}"
# Never execute a cross-compiled binary.
