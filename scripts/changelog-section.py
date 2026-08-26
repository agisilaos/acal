#!/usr/bin/env python3
"""Validate or extract one concrete CHANGELOG.md release section."""

from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path


HEADING_RE = re.compile(r"^## \[(v[0-9]+\.[0-9]+\.[0-9]+)\] - ([0-9]{4}-[0-9]{2}-[0-9]{2})$")
TRACE_RE = re.compile(
    r"https://github\.com/[^/\s]+/[^/\s]+/(?:pull/[0-9]+|commit/[0-9a-fA-F]{7,40})(?:[)>.,;:]|$)"
)


@dataclass(frozen=True)
class Section:
    version: str
    date: str
    body: str


def release_sections(text: str) -> list[Section]:
    lines = text.splitlines()
    starts: list[tuple[int, re.Match[str]]] = []
    for index, line in enumerate(lines):
        match = HEADING_RE.match(line)
        if match:
            starts.append((index, match))

    sections: list[Section] = []
    for position, (start, match) in enumerate(starts):
        end = starts[position + 1][0] if position + 1 < len(starts) else len(lines)
        body = "\n".join(lines[start + 1 : end]).strip()
        sections.append(Section(match.group(1), match.group(2), body))
    return sections


def bullet_blocks(body: str) -> list[str]:
    blocks: list[list[str]] = []
    current: list[str] | None = None
    for line in body.splitlines():
        if line.startswith("- "):
            if current is not None:
                blocks.append(current)
            current = [line]
        elif current is not None and (line.startswith("  ") or not line.strip()):
            current.append(line)
        elif current is not None:
            blocks.append(current)
            current = None
    if current is not None:
        blocks.append(current)
    return ["\n".join(block) for block in blocks]


def fail(message: str) -> int:
    print(f"error: {message}", file=sys.stderr)
    return 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--file", default="CHANGELOG.md")
    parser.add_argument("--validate", action="store_true")
    parser.add_argument("--extract", action="store_true")
    parser.add_argument("--require-traceability", action="store_true")
    args = parser.parse_args()

    if not args.validate and not args.extract:
        return fail("one of --validate or --extract is required")
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", args.version):
        return fail(f"version must match vX.Y.Z (got: {args.version})")

    path = Path(args.file)
    if not path.exists():
        return fail(f"{path} not found")
    text = path.read_text(encoding="utf-8")
    if re.search(r"^## \[Unreleased\]", text, flags=re.MULTILINE):
        return fail(f"{path} must not contain ## [Unreleased]")

    sections = release_sections(text)
    if not sections:
        return fail(f"{path} has no release heading in format ## [vX.Y.Z] - YYYY-MM-DD")
    if sections[0].version != args.version:
        return fail(f"{path} top release heading must be ## [{args.version}] - YYYY-MM-DD")

    section = sections[0]
    bullets = bullet_blocks(section.body)
    if not bullets:
        return fail(f"{path} section for {args.version} must contain at least one bullet")

    if args.require_traceability:
        missing = [bullet.splitlines()[0] for bullet in bullets if not TRACE_RE.search(bullet)]
        if missing:
            print(
                "error: every changelog bullet must link to a GitHub pull request or commit:",
                file=sys.stderr,
            )
            for bullet in missing:
                print(f"  {bullet}", file=sys.stderr)
            return 1

    if args.extract:
        print(section.body)
    if args.validate:
        print(f"changelog section valid for {args.version}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
