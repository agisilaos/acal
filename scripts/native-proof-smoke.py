#!/usr/bin/env python3
"""Install a native proof archive and verify sequential, disposable fixture operations."""

import argparse
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import tarfile
import tempfile
import time
import uuid


def extract_archive(archive, destination):
    # Proof archives contain regular files/directories only; reject links and escapes.
    with tarfile.open(archive, 'r:gz') as source:
        for member in source.getmembers():
            relative = PurePosixPath(member.name)
            if relative.is_absolute() or '..' in relative.parts or not (member.isdir() or member.isfile()):
                raise ValueError('archive contains an unsupported path or entry')
            target = destination.joinpath(*relative.parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with source.extractfile(member) as reader, target.open('wb') as writer:
                    shutil.copyfileobj(reader, writer)
                target.chmod(member.mode & 0o777)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    installation = parser.add_mutually_exclusive_group(required=True)
    installation.add_argument('--archive', type=Path, help='Extract and test an archive')
    installation.add_argument('--binary', type=Path, help='Test an already installed binary or Homebrew symlink')
    selection = parser.add_mutually_exclusive_group(required=True)
    selection.add_argument('--local', action='store_true', help='Require exactly one writable local calendar')
    selection.add_argument('--calendar', help='Exact writable native calendar ID, selected explicitly')
    parser.add_argument('--allow-calendar-writes', action='store_true')
    args = parser.parse_args()
    if not args.allow_calendar_writes:
        parser.error('--allow-calendar-writes is required; creates and removes one owned fixture')
    if not __debug__:
        parser.error('validation requires Python assertions; do not use -O or PYTHONOPTIMIZE')
    archive = args.archive.resolve(strict=True) if args.archive else None
    workspace = Path(tempfile.mkdtemp(prefix='acal-native-smoke-'))
    workspace.chmod(0o700)
    print(f'Evidence: {workspace}', flush=True)
    if archive:
        install = workspace / 'install'
        extract_archive(archive, install)
        binary = install / 'bin/acal'
    else:
        # Execute the public symlink, but locate metadata beside its real binary.
        binary = args.binary.absolute()
        install = binary.resolve(strict=True).parent.parent
    build = json.loads((install / 'BUILD-INFO.json').read_text())
    state = workspace / 'state'
    environment = {k: v for k, v in os.environ.items() if not k.startswith('ACAL_')}
    environment['XDG_CONFIG_HOME'] = str(workspace / 'config')
    records = []
    event_id = None
    initial_id = None
    create_attempted = False
    delete_attempted = False
    deleted = False
    title = 'acal-native-smoke-' + uuid.uuid4().hex

    def save(name, value):
        target = workspace / name
        target.write_text(json.dumps(value, indent=2) + '\n')
        target.chmod(0o600)

    def cli(*words, expected=0, redaction=None):
        started = time.monotonic()
        result = subprocess.run([str(binary), 'native', *words, '--json', '--no-input'],
                                cwd=workspace, env=environment, capture_output=True,
                                text=True, timeout=25)
        response = json.loads(result.stdout) if result.stdout else None
        recorded = json.loads(json.dumps(response))
        if redaction == 'calendars' and recorded and 'data' in recorded:
            # Retain source/writability counts, never personal names or account IDs.
            for index, calendar in enumerate(recorded['data']['calendars']):
                calendar['id'] = f'[calendar-{index}]'
                calendar['name'] = '[redacted]'
        if redaction == 'events' and recorded and 'data' in recorded:
            recorded['data']['events'] = [
                e if e.get('id') == event_id else {'event': '[redacted]'}
                for e in recorded['data']['events']]
        safe_words = list(words)
        if '--calendar' in safe_words:
            safe_words[safe_words.index('--calendar') + 1] = '[selected-calendar]'
        records.append(dict(args=safe_words + ['--json', '--no-input'],
                            exit=result.returncode, response=recorded, stderr=result.stderr,
                            seconds=round(time.monotonic() - started, 4)))
        save('commands.json', records)
        print(f'{"PASS" if result.returncode == expected else "FAIL"}: {" ".join(words[:2])} (exit {result.returncode})', flush=True)
        if result.returncode != expected:
            raise RuntimeError('unexpected command result; inspect retained commands.json')
        return response

    def check_event(response, expected_title, offset):
        data = response['data']
        assert data['id'] == initial_id, 'event ID changed without an identity change'
        assert data['title'] == expected_title, 'title readback differs'
        expected = [] if offset is None else [{'type': 0, 'relative_seconds': offset}]
        assert data['alarms'] == expected, 'alarm type/offset/count readback differs'
        assert data['start'] == start_text and data['end'] == end_text, 'event dates changed'
        assert not data['recurring'] and not data['all_day'], 'fixture classification changed'

    def verify_absent():
        response = cli('events', 'show', event_id, expected=6)
        assert response['error']['code'] == 'NOT_FOUND', 'absence not established'

    version = subprocess.run([str(binary), 'version'], cwd=workspace, env=environment,
                             capture_output=True, text=True, check=True).stdout.strip()
    assert build['version'] in version, 'binary version does not match build manifest'
    save('environment.json', dict(archive_sha256=hashlib.sha256(archive.read_bytes()).hexdigest() if archive else None,
                                 binary=str(binary), real_binary=str(binary.resolve()),
                                 build=build, version=version,
                                 os=subprocess.check_output(['sw_vers'], text=True),
                                 architecture=os.uname().machine,
                                 scope='owned independent fixture; no production undo/redo'))
    cli('setup')
    calendars = cli('calendars', redaction='calendars')['data']['calendars']
    if args.local:
        candidates = [c for c in calendars if c['writable'] and c['source_type'] == 0]
    else:
        candidates = [c for c in calendars if c['writable'] and c['id'] == args.calendar]
    if len(candidates) != 1:
        raise RuntimeError('calendar selection is absent or ambiguous; explicitly select a writable calendar')
    calendar = candidates[0]['id']
    start = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(days=7)).replace(hour=10, minute=0, second=0, microsecond=0)
    end = start + datetime.timedelta(minutes=30)
    instant = lambda date: date.isoformat().replace('+00:00', 'Z')
    start_text, end_text = instant(start), instant(end)
    manifest = dict(title=title, state=str(state), start=start_text, end=end_text,
                    calendar_id=calendar, source_type=candidates[0]['source_type'],
                    status='intent; creation not yet attempted')
    save('fixture.json', manifest)
    try:
        create_attempted = True
        result = cli('events', 'add', '--state-dir', str(state), '--calendar', calendar,
                     '--title', title, '--start', start_text, '--end', end_text, '--before', '15m')
        event_id = initial_id = result['data']['id']
        manifest.update(id=event_id, status='created'); save('fixture.json', manifest)
        check_event(result, title, -900)
        for _ in range(3):
            check_event(cli('events', 'show', event_id), title, -900)
            listing = cli('events', 'list', '--calendar', calendar,
                          '--from', start_text, '--to', end_text, '--limit', '1000', redaction='events')['data']
            assert not listing['truncated'], 'cannot verify equality against a truncated listing'
            matches = [e for e in listing['events'] if e['id'] == initial_id]
            assert len(matches) == 1, 'list and show must return the same exact ID'
            check_event({'data': matches[0]}, title, -900)
        # The previous encoder's valid references had arbitrary key order.
        fields = json.loads(base64.b64decode(event_id.split('.', 1)[1]))
        old_payload = json.dumps({k: fields[k] for k in ['start', 'item', 'calendar']})
        old_id = 'ekp1.' + base64.b64encode(old_payload.encode()).decode()
        check_event(cli('events', 'show', old_id), title, -900)
        rejection = cli('events', 'update', event_id, '--state-dir', str(workspace / 'wrong-state'),
                        '--title', 'must-not-apply', expected=6)
        assert rejection['outcome'] == 'rejected' and rejection['error']['code'] == 'NOT_OWNED'
        check_event(cli('events', 'show', event_id), title, -900)
        title += ' edited'
        check_event(cli('events', 'update', event_id, '--state-dir', str(state), '--title', title), title, -900)
        for before, offset in [('30m', -1800), ('0', 0)]:
            check_event(cli('events', 'remind', event_id, '--state-dir', str(state), '--before', before), title, offset)
            check_event(cli('events', 'show', event_id), title, offset)
        check_event(cli('events', 'remind', event_id, '--state-dir', str(state), '--clear'), title, None)
        check_event(cli('events', 'show', event_id), title, None)
        delete_attempted = True
        cli('events', 'delete', event_id, '--state-dir', str(state))
        verify_absent(); deleted = True
        manifest.update(status='deleted; absence verified'); save('fixture.json', manifest)
        save('result.json', dict(passed=True, cleanup='verified',
                                checks=['ID equality across processes/list/show/edit', 'old ID decoding',
                                        'reminder create/replace/at-start/clear', 'ownership rejection', 'delete absence']))
        print('PASS: installed fixture workflow; cleanup verified', flush=True)
    except Exception as error:
        save('result.json', dict(passed=False, error=str(error)))
        raise
    finally:
        if event_id and not deleted:
            # Never retry a potentially applied delete. A read can establish absence.
            if not delete_attempted:
                delete_attempted = True
                try:
                    cli('events', 'delete', event_id, '--state-dir', str(state))
                except Exception:
                    pass
            try:
                verify_absent()
                manifest.update(status='deleted; absence verified during cleanup')
            except Exception:
                manifest.update(status='cleanup uncertain; inspect exact recorded fixture, do not retry blindly')
            save('fixture.json', manifest)
        elif create_attempted and not event_id:
            manifest.update(status='creation outcome requires inspection; retain operation records; do not retry add')
            save('fixture.json', manifest)


if __name__ == '__main__':
    main()
