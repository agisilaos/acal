#!/usr/bin/env python3
"""Check the common publisher documentation contract without running examples."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit


REQUIRED_HEADINGS = ["Install", "Usage", "Release", "Docs"]
REQUIRED_RELEASE_LINES = [
    "make changelog-context VERSION=vX.Y.Z",
    "make release-check VERSION=vX.Y.Z",
    "make release-dry-run VERSION=vX.Y.Z",
    "make release VERSION=vX.Y.Z",
    "scripts/changelog-context.sh",
    "scripts/release-check.sh",
    "scripts/release.sh",
    "RELEASING.md",
]
INLINE_LINK_RE = re.compile(r"!?\[[^\]]*\]\(\s*(<[^>]+>|[^\s)]+)(?:\s+\"[^\"]*\"|\s+'[^']*')?\s*\)")
REFERENCE_RE = re.compile(r"!?\[([^\]]+)\]\[([^\]]*)\]")
DEFINITION_RE = re.compile(r"^\s{0,3}\[([^\]]+)\]:\s*(<[^>]+>|\S+)")


def visible_lines(text: str) -> list[tuple[int, str]]:
    result = []
    fence: tuple[str, int] | None = None
    for number, line in enumerate(text.splitlines(), 1):
        marker = re.match(r"^\s*(`{3,}|~{3,})(.*)$", line)
        if fence is not None:
            if marker and marker.group(1)[0] == fence[0] and len(marker.group(1)) >= fence[1] and not marker.group(2).strip():
                fence = None
            continue
        if marker:
            fence = (marker.group(1)[0], len(marker.group(1)))
            continue
        result.append((number, re.sub(r"(`+).*?\1", "", line)))
    return result


def link_error(root: Path, path: Path, number: int, target: str) -> str | None:
    target = target.strip("<>")
    label = f"{path.relative_to(root)}:{number}"
    try:
        link = urlsplit(target)
    except ValueError as exc:
        return f"{label}: invalid link target: {target} ({exc})"
    if link.scheme or link.netloc or not link.path:
        return None
    destination = path.parent / unquote(link.path)
    try:
        destination.resolve().relative_to(root)
    except ValueError:
        return f"{label}: local link escapes the repository: {target}"
    if not destination.exists():
        return f"{label}: missing link target: {target}"
    return None


def local_link_errors(root: Path, path: Path, text: str) -> list[str]:
    lines = visible_lines(text)
    definitions = {}
    for number, line in lines:
        match = DEFINITION_RE.match(line)
        if match:
            definitions[match.group(1).casefold()] = match.group(2)
    errors = []
    for number, line in lines:
        targets = INLINE_LINK_RE.findall(line)
        definition = DEFINITION_RE.match(line)
        if definition:
            targets.append(definition.group(2))
        else:
            for label, reference in REFERENCE_RE.findall(line):
                target = definitions.get((reference or label).casefold())
                if target is None:
                    errors.append(f"{path.relative_to(root)}:{number}: reference link has no definition: {reference or label}")
                else:
                    targets.append(target)
        for target in targets:
            error = link_error(root, path, number, target)
            if error:
                errors.append(error)
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".", help="Consumer repository root (default: current directory)")
    args = parser.parse_args()
    root = Path(args.root).resolve()
    errors = []
    try:
        readme = root / "README.md"
        if not readme.is_file():
            raise ValueError("README.md not found")
        text = readme.read_text(encoding="utf-8")
        for name in ("CHANGELOG.md", "RELEASING.md"):
            if not (root / name).is_file():
                errors.append(f"{name} not found")
        headings = [match.group(1) for _, line in visible_lines(text) if (match := re.fullmatch(r"##\s+(.+?)\s*", line))]
        if [heading for heading in headings if heading in REQUIRED_HEADINGS] != REQUIRED_HEADINGS:
            errors.append("README.md headings must appear once in order: " + ", ".join(REQUIRED_HEADINGS))
        for reference in REQUIRED_RELEASE_LINES:
            if reference not in text:
                errors.append(f"README.md missing release reference: {reference}")
        documents = sorted(root.glob("*.md")) + sorted((root / "docs").rglob("*.md"))
        for path in documents:
            contents = path.read_text(encoding="utf-8")
            errors.extend(local_link_errors(root, path, contents))
            for number, line in visible_lines(contents):
                if re.match(r"^##\s+\[Unreleased\]", line, re.IGNORECASE):
                    errors.append(f"{path.relative_to(root)}:{number}: concrete release sections are required; Unreleased is forbidden")
        if (root / "CHANGELOG.md").is_file():
            result = subprocess.run([sys.executable, str(Path(__file__).with_name("changelog-section.py")), "--file", str(root / "CHANGELOG.md"), "--latest"], capture_output=True, text=True)
            if result.returncode:
                errors.append(result.stderr.strip() or "CHANGELOG.md validation failed")
    except (OSError, UnicodeError, ValueError) as exc:
        errors.append(str(exc))
    if errors:
        for error in errors:
            print(f"error: {error}", file=sys.stderr)
        return 1
    print("docs contract check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
