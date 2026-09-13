#!/usr/bin/env bash
# dev/e2e-transfer.sh: End-to-end drill for resumable transfer with failure injection and recovery.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

TEST_DIR=$(mktemp -d /tmp/e2e-transfer-XXXXXX)
trap 'rm -rf "${TEST_DIR}"' EXIT

echo "=== AutoGet Resumable Upload E2E Drill ==="
echo "Working directory: ${TEST_DIR}"

COMPLETED_DIR="${TEST_DIR}/completed"
UPLOAD_DIR="${COMPLETED_DIR}/.uploads"
SRC_DIR="${TEST_DIR}/src"
mkdir -p "${COMPLETED_DIR}" "${SRC_DIR}"

# 1. Generate 50MB test file (fast test file)
TEST_FILE="${SRC_DIR}/large_test.bin"
echo "Generating 50MB random test data..."
head -c 52428800 </dev/urandom > "${TEST_FILE}"
EXPECTED_SHA=$(sha256sum "${TEST_FILE}" | awk '{print $1}')
echo "Expected SHA256: ${EXPECTED_SHA}"

# 2. Build organizer if needed
ORGANIZER_BIN="${REPO_ROOT}/organizer/bin/organizer"
if [[ ! -f "${ORGANIZER_BIN}" ]]; then
    echo "Building organizer..."
    (cd "${REPO_ROOT}/organizer" && go build -o bin/organizer ./cmd/server)
fi

PORT=18080
export PORT
export DOWNLOAD_COMPLETED_DIR="${COMPLETED_DIR}"
export TARGET_DIR="${TEST_DIR}/target"
export JAV_ACTOR_FILE="${TEST_DIR}/actors.json"
export FLARESOLVERR_URL="http://localhost:8191"
export TMDB_API_KEY="dummy-tmdb"
export METATUBE_API_URL="http://localhost:9000"
export MODEL="xai:grok-2"
export XAI_API_KEY="dummy-xai"
export UPLOAD_TEMP_DIR="${UPLOAD_DIR}"
export UPLOAD_RESERVE_BYTES="1048576"

mkdir -p "${TARGET_DIR}"
for d in movie tv_series porn jav soft_porn anim_movie anim_tv_series; do
    mkdir -p "${TARGET_DIR}/${d}"
done
touch "${JAV_ACTOR_FILE}"

echo "Starting Organizer server on :${PORT}..."
"${ORGANIZER_BIN}" &
SERVER_PID=$!
trap 'kill -9 "${SERVER_PID}" 2>/dev/null || true; rm -rf "${TEST_DIR}"' EXIT

# Wait for server readiness
for i in {1..30}; do
    if curl -sf "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1; then
        echo "Organizer server is healthy."
        break
    fi
    sleep 0.2
done

# Run transfer drill using go run
echo "Running resumable transfer drill..."
(
    cd "${REPO_ROOT}"
    go run -v ./backend/cmd/e2e_transfer_runner.go \
        -server "http://127.0.0.1:${PORT}" \
        -file "${TEST_FILE}" \
        -torrent "e2e-torrent-1" \
        -rel "videos/large_test.bin" \
        -target "${COMPLETED_DIR}/e2e-torrent-1/videos/large_test.bin" \
        -expected-sha "${EXPECTED_SHA}"
)

echo "=== Resumable Upload E2E Drill Passed Successfully! ==="
