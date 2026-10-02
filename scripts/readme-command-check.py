#!/usr/bin/env python3
"""Check documented command paths and flags using metadata, without executing examples."""

import argparse
import json
from pathlib import Path
import shlex
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]


def load_inventory():
    result = subprocess.run(['go', 'run', './tools/docs-command-inventory'], cwd=ROOT,
                            capture_output=True, text=True, check=True)
    return json.loads(result.stdout)


def check_tokens(tokens, inventory):
    """Validate groups, leaf operands and flags; never call a command handler."""
    if not tokens or tokens[0] not in ('acal', './acal'):
        raise ValueError('expected acal command')
    path = 'acal'
    operands = False
    index = 1
    while index < len(tokens):
        token = tokens[index]
        entry = inventory[path]
        flags = entry['flags']
        if token == '--':
            if not entry['accepts_operands'] and index + 1 < len(tokens):
                raise ValueError(f'{path} requires a known subcommand before operands')
            return
        if token.startswith('--'):
            name, separator, value = token[2:].partition('=')
            if name not in flags:
                raise ValueError(f'unknown flag --{name} for {path}')
            flag = flags[name]
            if separator and flag['type'] == 'bool' and value.lower() not in ('1', 't', 'true', '0', 'f', 'false'):
                raise ValueError(f'invalid boolean value for --{name}')
            if not separator and not flag['no_value']:
                index += 1
                if index >= len(tokens) or tokens[index].startswith('--'):
                    raise ValueError(f'--{name} requires a value')
            index += 1
            continue
        if token.startswith('-') and token != '-':
            short = {f['shorthand']: f for f in flags.values() if f['shorthand']}
            remaining = token[1:]
            while remaining:
                name, remaining = remaining[0], remaining[1:]
                if name not in short:
                    raise ValueError(f'unknown flag -{name} for {path}')
                if not short[name]['no_value']:
                    if not remaining:
                        index += 1
                        if index >= len(tokens) or tokens[index].startswith('--'):
                            raise ValueError(f'-{name} requires a value')
                    break
                if remaining.startswith('='):
                    if remaining[1:].lower() not in ('1', 't', 'true', '0', 'f', 'false'):
                        raise ValueError(f'invalid boolean value for -{name}')
                    break
            index += 1
            continue
        child = entry['children'].get(token) if not operands else None
        if child:
            path += ' ' + child
        elif not entry['accepts_operands']:
            raise ValueError(f'unknown subcommand {token!r} for {path}')
        else:
            operands = True
        index += 1


def check_readme(readme, inventory):
    count = 0
    errors = []
    for line_no, line in enumerate(readme.splitlines(), start=1):
        text = line.strip()
        if not (text.startswith('acal ') or text.startswith('./acal ')):
            continue
        count += 1
        try:
            check_tokens(shlex.split(text, comments=True), inventory)
        except ValueError as error:
            errors.append(f'line {line_no}: {error}: {text}')
    if not count:
        errors.append('no CLI command examples found')
    if errors:
        raise ValueError('\n'.join(errors))
    return count


def check_snapshots(manifest, inventory, help_dir):
    expected = set(inventory)
    seen_paths = set()
    seen_files = set()
    for line_no, line in enumerate(manifest.splitlines(), start=1):
        if not line or line.startswith('#'):
            continue
        try:
            filename, args = line.split('\t')
        except ValueError as error:
            raise ValueError(f'help manifest line {line_no} requires file<TAB>args') from error
        relative = Path(filename)
        if len(relative.parts) != 1 or relative.suffix != '.txt' or filename in seen_files:
            raise ValueError(f'invalid or duplicate snapshot filename: {filename}')
        tokens = shlex.split(args)
        if not tokens or tokens[-1] != '--help' or any(t.startswith('-') for t in tokens[:-1]):
            raise ValueError(f'snapshot must use only a command path and --help: {args}')
        path = ' '.join(['acal', *tokens[:-1]])
        if path not in expected or path in seen_paths:
            raise ValueError(f'unknown or duplicate snapshot command: {path}')
        if not (help_dir / filename).is_file():
            raise ValueError(f'missing help snapshot: {filename}')
        seen_files.add(filename)
        seen_paths.add(path)
    if seen_paths != expected:
        raise ValueError('help manifest missing command paths: ' + ', '.join(sorted(expected - seen_paths)))
    return len(seen_paths)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--readme', type=Path, default=ROOT / 'README.md')
    args = parser.parse_args()
    try:
        inventory = load_inventory()
        examples = check_readme(args.readme.read_text(), inventory)
        snapshots = check_snapshots((ROOT / 'scripts/help-snapshots.txt').read_text(), inventory, ROOT / 'docs/help')
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'error: docs command inventory: {error}', file=sys.stderr)
        return 1
    print(f'[docs-check] verified {examples} README examples and {snapshots} help paths without executing examples')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
