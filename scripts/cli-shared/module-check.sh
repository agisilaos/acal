#!/usr/bin/env bash
set -euo pipefail

# Run from the consumer root, including on Go 1.22 without tidy -diff.
[[ $# -eq 0 ]] || { printf 'usage: module-check.sh (run from module root)\n' >&2; exit 2; }
[[ -f go.mod && ! -L go.mod ]] || { printf 'error: go.mod must be a regular file in the current directory\n' >&2; exit 1; }
[[ ! -e go.sum && ! -L go.sum || -f go.sum && ! -L go.sum ]] || { printf 'error: go.sum must be absent or a regular file\n' >&2; exit 1; }

work="$(mktemp -d "${TMPDIR:-/tmp}/cli-module-check.XXXXXX")"
tidy_pid=""
cleanup() {
  rm -rf "$work"
}
on_signal() {
  local status="$1"
  trap '' HUP INT TERM
  if [[ -n "$tidy_pid" ]]; then
    kill -TERM "$tidy_pid" 2>/dev/null || true
    wait "$tidy_pid" 2>/dev/null || true
  fi
  printf 'error: module validation interrupted; original module files were not written\n' >&2
  exit "$status"
}
trap cleanup EXIT
trap 'on_signal 129' HUP
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

cp go.mod "$work/check.mod"
[[ ! -f go.sum ]] || cp go.sum "$work/check.sum"
GOWORK=off go mod tidy -modfile="$work/check.mod" &
tidy_pid=$!
if wait "$tidy_pid"; then
  tidy_pid=""
else
  status=$?
  tidy_pid=""
  printf 'error: go mod tidy failed; original module files were not written\n' >&2
  exit "$status"
fi

status=0
diff -u go.mod "$work/check.mod" || status=1
before_sum=go.sum
after_sum="$work/check.sum"
[[ -f "$before_sum" ]] || before_sum=/dev/null
[[ -f "$after_sum" ]] || after_sum=/dev/null
diff -u "$before_sum" "$after_sum" || status=1
if [[ "$status" -ne 0 ]]; then
  printf 'error: go.mod/go.sum drift detected; run go mod tidy\n' >&2
  exit 1
fi
printf 'module metadata is tidy\n'
