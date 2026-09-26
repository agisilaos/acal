#!/usr/bin/env python3
"""Opt-in, disposable Calendar smoke test for a built acal binary."""

import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import tempfile
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=Path)
    parser.add_argument('--allow-calendar-writes', action='store_true')
    parser.add_argument('--timeout', type=int, default=90, help='seconds per CLI call')
    args = parser.parse_args()
    if not args.allow_calendar_writes:
        parser.error('--allow-calendar-writes is required; this creates and removes a test calendar')
    if args.timeout <= 0:
        parser.error('--timeout must be positive')
    binary = args.binary.resolve(strict=True)
    workspace = Path(tempfile.mkdtemp(prefix='acal-calendar-smoke-'))
    name = 'acal-release-smoke-' + uuid.uuid4().hex
    env = {k: v for k, v in os.environ.items() if not k.startswith('ACAL_')}
    env.update(XDG_CONFIG_HOME=str(workspace / 'config'), ACAL_BACKEND='osascript', ACAL_OUTPUT='json')
    results = []
    print(f'Evidence: {workspace}', flush=True)

    def native(source, *values):
        return subprocess.run(
            ['/usr/bin/osascript', '-s', 'h', '-e', source, '--', *values],
            capture_output=True, text=True, timeout=30, check=True,
        ).stdout.strip()

    def cli(*words):
        result = subprocess.run(
            [str(binary), *words, '--json', '--timeout', f'{args.timeout}s', '--no-input'],
            cwd=workspace, env=env, capture_output=True, text=True,
            timeout=args.timeout + 10,
        )
        results.append(dict(args=words, exit=result.returncode, stdout=result.stdout, stderr=result.stderr))
        (workspace / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
        if result.returncode:
            raise RuntimeError(f'{words}: {result.stderr or result.stdout}')
        print(f'PASS: {words[0]} {words[1] if len(words) > 1 else ""}', flush=True)
        return json.loads(result.stdout)['data']

    version = subprocess.check_output([str(binary), 'version'], text=True, cwd=workspace, env=env).strip()
    (workspace / 'identity.json').write_text(json.dumps(dict(binary=str(binary), version=version, calendar=name), indent=2))
    native('''use framework "EventKit"
if (current application's EKEventStore's authorizationStatusForEntityType:0) as integer is not 3 then error "Enable Full Calendar Access for the invoking app before running this test"''')
    # Cleanup is registered before creation: Calendar may save before a command fails.
    try:
        native('''on run argv
tell application "Calendar"
make new calendar with properties {name:item 1 of argv}
end tell
end run''', name)
        start = (datetime.datetime.now().astimezone() + datetime.timedelta(days=7)).replace(hour=10, minute=0, second=0, microsecond=0)
        end = start + datetime.timedelta(minutes=30)
        event = cli('events', 'add', '--calendar', name, '--title', 'acal release smoke', '--start', start.isoformat(), '--end', end.isoformat())
        event_id = event['id']
        observed = cli('events', 'show', event_id)
        if datetime.datetime.fromisoformat(observed['start']) != start:
            raise RuntimeError('Stored event start differs from requested instant')
        updated = cli('events', 'update', event_id, '--title', 'acal release smoke updated', '--scope', 'this')
        if updated['title'] != 'acal release smoke updated':
            raise RuntimeError('Updated title not observed')
        event_id = updated['id']
        cli('events', 'remind', event_id, '--at=-15m')
        cli('history', 'undo')
        cli('history', 'redo')
        cli('events', 'remind', event_id, '--at=-30m')
        cli('events', 'remind', event_id, '--clear')
        cli('history', 'undo')
        cli('history', 'redo')
        cli('events', 'delete', event_id, '--force', '--scope', 'this')
        remaining = native('''on run argv
tell application "Calendar" to return count of events of (first calendar whose name is item 1 of argv)
end run''', name)
        if remaining != '0':
            raise RuntimeError('Deleted event remains in the disposable calendar')
    except Exception as error:
        (workspace / 'failure.txt').write_text(str(error) + '\n')
        raise
    finally:
        cleanup = native('''on run argv
tell application "Calendar"
set matches to every calendar whose name is item 1 of argv
if (count of matches) is 0 then return "absent"
if (count of matches) is not 1 then error "Ambiguous disposable calendar; inspect manually"
delete (first calendar whose name is item 1 of argv)
return "removed"
end tell
end run''', name)
        (workspace / 'cleanup.txt').write_text(cleanup + '\n')
        print(f'Disposable calendar: {cleanup}', flush=True)
    print(f'PASS: full Calendar workflow ({version})', flush=True)


if __name__ == '__main__':
    main()
