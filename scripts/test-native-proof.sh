#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT
xcrun swiftc -swift-version 5 -framework EventKit \
  "$ROOT_DIR/native/EventKitHelper/Core.swift" "$ROOT_DIR/native/tests/main.swift" \
  -o "$TEST_DIR/native-tests"
# Separate processes also exercise per-process hash/key ordering differences.
for attempt in 1 2 3; do "$TEST_DIR/native-tests"; done

# Compile the production entry point too; reject malformed protocol without access.
xcrun swiftc -swift-version 5 -framework EventKit \
  "$ROOT_DIR/native/EventKitHelper/Core.swift" "$ROOT_DIR/native/EventKitHelper/main.swift" \
  -o "$TEST_DIR/acal-native"
python3 - "$TEST_DIR/acal-native" <<'PYTEST'
import json, subprocess, sys
request = dict(protocol="unsupported", request_id="a" * 32, operation="add", args={})
r = subprocess.run([sys.argv[1]], input=json.dumps(request), capture_output=True, text=True, check=True)
response = json.loads(r.stdout)
assert response["outcome"] == "rejected"
assert response["error"]["code"] == "PROTOCOL_ERROR"
print("PASS: helper protocol rejected before Calendar access")
PYTEST
