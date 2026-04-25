#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
LINT_IMAGE="${GO_LINT_IMAGE:-llrdc-client:go-lint}"

cd "${ROOT_DIR}"

echo "▶ Checking Go formatting..."
unformatted="$(gofmt -l $(git ls-files '*.go'))"
if [ -n "${unformatted}" ]; then
  echo "Go files need gofmt:"
  echo "${unformatted}"
  exit 1
fi

echo "▶ Building Go lint environment..."
docker build -f "${ROOT_DIR}/Dockerfile.client" --target builder -t "${LINT_IMAGE}" "${ROOT_DIR}"

echo "▶ Running go vet across the repo..."
docker run --rm -v "${ROOT_DIR}:/app" -w /app "${LINT_IMAGE}" \
  sh -lc 'CGO_ENABLED=1 /usr/local/go/bin/go vet ./...'

echo "Go lint passed"
