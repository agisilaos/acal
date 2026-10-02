#!/usr/bin/env python3
"""Validate or extract the first concrete, reviewed CHANGELOG.md section."""

from __future__ import annotations

import argparse
import datetime as dt
import re
import sys
from dataclasses import dataclass
from pathlib import Path


VERSION_RE = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+\Z")
HEADING_RE = re.compile(r"## \[(v[0-9]+\.[0-9]+\.[0-9]+)\] - ([0-9]{4}-[0-9]{2}-[0-9]{2})\Z")
H2_RE = re.compile(r"^ {0,3}##\s+(.+?)\s*$")
RELEASE_LIKE_RE = re.compile(r"^(?:\[|v?[0-9]|unreleased\b)", re.IGNORECASE)
ITEM_RE = re.compile(r"^(\s*)(?:[-*+]|[0-9]+[.)])\s+(\S.*)$")
MALFORMED_ITEM_RE = re.compile(r"^\s*(?:[-*+]|[0-9]+[.)])")
TRACE_RE = re.compile(
    r"https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/"
    r"(?:pull/[1-9][0-9]*|commit/[0-9a-fA-F]{7,40})(?=$|[\s)\]>,.;:#?])"
)


@dataclass(frozen=True)
class Section:
    version: str
    date: str
    body: str


def visible_lines(text: str) -> list[tuple[int, str]]:
    """Return non-fenced lines with their original indexes."""
    result = []
    fence: tuple[str, int] | None = None
    for index, line in enumerate(text.splitlines()):
        marker = re.match(r"^\s*(`{3,}|~{3,})(.*)$", line)
        if fence is not None:
            if marker and marker.group(1)[0] == fence[0] and len(marker.group(1)) >= fence[1] and not marker.group(2).strip():
                fence = None
            continue
        if marker:
            fence = (marker.group(1)[0], len(marker.group(1)))
            continue
        result.append((index, line))
    return result


def release_sections(text: str) -> list[Section]:
    lines = text.splitlines()
    headings = []
    starts = []
    for index, line in visible_lines(text):
        heading = H2_RE.fullmatch(line)
        if not heading:
            if re.match(r"^ {0,3}##(?=[^\s#])", line) and RELEASE_LIKE_RE.match(line.lstrip()[2:]):
                raise ValueError(f"line {index + 1}: malformed release heading: {line}")
            continue
        headings.append(index)
        match = HEADING_RE.fullmatch(line)
        if match is None:
            if RELEASE_LIKE_RE.match(heading.group(1)):
                raise ValueError(f"line {index + 1}: release heading must use ## [vX.Y.Z] - YYYY-MM-DD (got: {line})")
            continue
        try:
            dt.date.fromisoformat(match.group(2))
        except ValueError as exc:
            raise ValueError(f"line {index + 1}: invalid release date: {match.group(2)}") from exc
        starts.append((index, match))
    sections = []
    for start, match in starts:
        end = next((index for index in headings if index > start), len(lines))
        sections.append(Section(match.group(1), match.group(2), "\n".join(lines[start + 1:end]).strip()))
    return sections


def bullet_blocks(body: str) -> list[str]:
    """Keep each nested or ordered item independent of its children."""
    blocks: list[list[str]] = []
    active: list[tuple[int, int]] = []
    for index, line in visible_lines(body):
        item = ITEM_RE.fullmatch(line)
        if item:
            indent = len(item.group(1).expandtabs(4))
            while active and active[-1][0] >= indent:
                active.pop()
            blocks.append([line])
            active.append((indent, len(blocks) - 1))
            continue
        if MALFORMED_ITEM_RE.match(line) and not re.fullmatch(r"\s*(?:-{3,}|\*{3,}|_{3,})\s*", line):
            raise ValueError(f"section line {index + 1}: malformed list item: {line}")
        if not line.strip():
            continue
        indent = len(line) - len(line.lstrip())
        while active and indent <= active[-1][0]:
            active.pop()
        if active:
            blocks[active[-1][1]].append(line)
    return ["\n".join(block) for block in blocks]


def first_section(text: str, version: str | None = None, require_traceability: bool = False) -> Section:
    sections = release_sections(text)
    if not sections:
        raise ValueError("no release heading in format ## [vX.Y.Z] - YYYY-MM-DD")
    section = sections[0]
    if version is not None and section.version != version:
        raise ValueError(f"top release heading must be ## [{version}] - YYYY-MM-DD")
    bullets = bullet_blocks(section.body)
    if not bullets:
        raise ValueError(f"section for {section.version} must contain at least one list item")
    if require_traceability:
        missing = [bullet.splitlines()[0] for bullet in bullets if not TRACE_RE.search(re.sub(r"(`+).*?\1", "", bullet))]
        if missing:
            raise ValueError("every changelog list item must link to a GitHub pull request or commit:\n  " + "\n  ".join(missing))
    return section


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version")
    parser.add_argument("--file", default="CHANGELOG.md")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--validate", action="store_true")
    mode.add_argument("--extract", action="store_true")
    mode.add_argument("--latest", action="store_true", help="Print the first validated concrete version; historical traceability is optional")
    parser.add_argument("--require-traceability", action="store_true")
    args = parser.parse_args()
    if args.latest and args.version:
        parser.error("--latest does not accept --version")
    if not args.latest and (not args.version or not VERSION_RE.fullmatch(args.version)):
        parser.error("--version must match vX.Y.Z")
    try:
        path = Path(args.file)
        section = first_section(path.read_text(encoding="utf-8"), args.version, args.require_traceability)
    except (OSError, UnicodeError, ValueError) as exc:
        print(f"error: {args.file}: {exc}", file=sys.stderr)
        return 1
    if args.latest:
        print(section.version)
    elif args.extract:
        print(section.body)
    else:
        print(f"changelog section valid for {section.version}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
