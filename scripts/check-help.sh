#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXPECTED_DIR="${ROOT_DIR}/docs/help"
SNAPSHOT_LIST="${ROOT_DIR}/scripts/help-snapshots.txt"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

if [[ -z "${GOCACHE:-}" ]]; then
  export GOCACHE="${TMP_DIR}/gocache"
fi

"${ROOT_DIR}/scripts/update-help.sh" --out-dir "${TMP_DIR}" >/dev/null

while IFS=$'\t' read -r file _ || [[ -n "${file:-}" ]]; do
  [[ -z "${file:-}" || "${file}" =~ ^# ]] && continue
  if ! diff -u "${EXPECTED_DIR}/${file}" "${TMP_DIR}/${file}"; then
    echo >&2
    echo "help output drift detected in ${file}" >&2
    echo "run: scripts/update-help.sh" >&2
    exit 1
  fi
done < "${SNAPSHOT_LIST}"

echo "help snapshots are up to date"
