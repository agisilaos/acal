#!/usr/bin/env python3
"""Enforce shared documentation contract for CLI repositories."""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

REQUIRED_HEADINGS = ["Install", "Usage", "Release", "Docs"]
REQUIRED_RELEASE_LINES = [
    "make release-check VERSION=vX.Y.Z",
    "make release-dry-run VERSION=vX.Y.Z",
    "make release VERSION=vX.Y.Z",
    "scripts/release-check.sh",
    "scripts/release.sh",
]
FORBIDDEN_PATTERNS = [
    re.compile(r"^## \[Unreleased\]", flags=re.MULTILINE),
]


def has_h2_heading(text: str, heading: str) -> bool:
    pattern = re.compile(rf"^##\s+{re.escape(heading)}\s*$", flags=re.MULTILINE)
    return bool(pattern.search(text))


def iter_docs_markdown(root: Path) -> list[Path]:
    files: list[Path] = []

    readme = root / "README.md"
    if readme.exists():
        files.append(readme)

    releasing = root / "RELEASING.md"
    if releasing.exists():
        files.append(releasing)

    docs_dir = root / "docs"
    if docs_dir.exists():
        files.extend(sorted(docs_dir.rglob("*.md")))

    return files


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate CLI docs contract")
    parser.add_argument("--root", default=".", help="Repository root (default: current directory)")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    readme = root / "README.md"

    if not readme.exists():
        print("error: README.md not found", file=sys.stderr)
        return 1

    readme_text = readme.read_text(encoding="utf-8")

    errors: list[str] = []

    for heading in REQUIRED_HEADINGS:
        if not has_h2_heading(readme_text, heading):
            errors.append(f"README.md missing required heading: ## {heading}")

    for line in REQUIRED_RELEASE_LINES:
        if line not in readme_text:
            errors.append(f"README.md missing release reference: {line}")

    for path in iter_docs_markdown(root):
        text = path.read_text(encoding="utf-8")
        rel = path.relative_to(root)
        for pattern in FORBIDDEN_PATTERNS:
            if pattern.search(text):
                errors.append(f"{rel} contains forbidden pattern: {pattern.pattern}")

    if errors:
        for err in errors:
            print(f"error: {err}", file=sys.stderr)
        return 1

    print("docs contract check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
