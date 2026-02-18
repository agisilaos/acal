#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: docs-check.sh must be run on macOS (Darwin)" >&2
  exit 1
fi

if [[ ! -f README.md ]]; then
  echo "error: README.md not found" >&2
  exit 1
fi

if [[ ! -f CHANGELOG.md ]]; then
  echo "error: CHANGELOG.md not found" >&2
  exit 1
fi

echo "[docs-check] validating README command examples against CLI help"
python3 - <<'PY'
import re
import shlex
import subprocess
import sys
from pathlib import Path


def run_help(path):
    cmd = ["go", "run", "./cmd/acal", *path, "--help"]
    p = subprocess.run(cmd, capture_output=True, text=True)
    if p.returncode != 0:
        raise RuntimeError(f"failed to run {' '.join(cmd)}: {p.stderr.strip()}")
    return p.stdout


def parse_children(help_text):
    children = []
    in_section = False
    for line in help_text.splitlines():
        if line.startswith("Available Commands:"):
            in_section = True
            continue
        if in_section:
            if not line.strip():
                continue
            if re.match(r"^(Flags|Global Flags|Additional help topics):", line):
                break
            m = re.match(r"^\s{2}([a-z0-9][a-z0-9-]*)\s{2,}", line)
            if m:
                name = m.group(1)
                if name != "help":
                    children.append(name)
    return children


valid_paths = {tuple()}
queue = [tuple()]
seen = set(queue)
while queue:
    path = queue.pop(0)
    text = run_help(list(path))
    for child in parse_children(text):
        nxt = (*path, child)
        valid_paths.add(nxt)
        if nxt not in seen:
            seen.add(nxt)
            queue.append(nxt)

readme = Path("README.md").read_text(encoding="utf-8").splitlines()
bad = []
count = 0
for i, line in enumerate(readme, start=1):
    s = re.sub(r"\s+#.*$", "", line.strip())
    if not s.startswith("./acal ") and not s.startswith("acal "):
        continue
    count += 1
    try:
        parts = shlex.split(s)
    except ValueError as exc:
        bad.append((i, s, f"failed to parse command: {exc}"))
        continue
    if not parts:
        continue
    if parts[0] in ("./acal", "acal"):
        parts = parts[1:]
    toks = []
    for tok in parts:
        if tok.startswith("-"):
            break
        toks.append(tok)
    matched = tuple()
    probe = []
    for tok in toks:
        probe.append(tok)
        cand = tuple(probe)
        if cand in valid_paths:
            matched = cand
        else:
            break
    if toks and not matched:
        bad.append((i, s, f"unknown command path prefix: {' '.join(toks)}"))

if count == 0:
    print("error: no CLI command examples found in README.md", file=sys.stderr)
    sys.exit(1)

if bad:
    print("error: README command examples out of date:", file=sys.stderr)
    for line_no, cmd, msg in bad:
        print(f"  README.md:{line_no}: {msg}: {cmd}", file=sys.stderr)
    sys.exit(1)

print(f"[docs-check] verified {count} README command examples")
PY

echo "[docs-check] checking roadmap completion markers"
if [[ -f docs/cli-expansion-roadmap.md ]]; then
  if rg -n -- '- \[ \] Step ' docs/cli-expansion-roadmap.md >/dev/null; then
    echo "error: docs/cli-expansion-roadmap.md still has unchecked steps" >&2
    exit 1
  fi
fi

echo "[docs-check] checking release script references in README"
if ! rg -q 'scripts/release-check.sh' README.md; then
  echo "error: README missing scripts/release-check.sh reference" >&2
  exit 1
fi
if ! rg -q 'scripts/release.sh' README.md; then
  echo "error: README missing scripts/release.sh reference" >&2
  exit 1
fi

echo "[docs-check] ok"
