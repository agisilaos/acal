#!/usr/bin/env python3
"""Qualify two native-proof archives through a temporary, task-owned Homebrew tap."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--old-package-dir', type=Path, required=True)
    parser.add_argument('--new-package-dir', type=Path, required=True)
    parser.add_argument('--allow-calendar-writes', action='store_true')
    parser.add_argument('--local', action='store_true', help='Require one writable local calendar for each smoke run')
    args = parser.parse_args()
    if not args.allow_calendar_writes or not args.local:
        parser.error('--allow-calendar-writes --local are required for this qualification')
    if not __debug__:
        parser.error('validation requires Python assertions')
    brew = shutil.which('brew')
    if not brew:
        parser.error('Homebrew is required')
    script_dir = Path(__file__).resolve().parent
    old, new = args.old_package_dir.resolve(strict=True), args.new_package_dir.resolve(strict=True)
    evidence = Path(tempfile.mkdtemp(prefix='acal-homebrew-evidence-'))
    evidence.chmod(0o700)
    print(f'Evidence: {evidence}', flush=True)
    env = dict(os.environ, HOMEBREW_NO_AUTO_UPDATE='1', HOMEBREW_NO_INSTALL_CLEANUP='1',
               HOMEBREW_NO_INSTALLED_DEPENDENTS_CHECK='1', HOMEBREW_NO_AUTOREMOVE='1',
               HOMEBREW_NO_ANALYTICS='1', HOMEBREW_NO_ASK='1',
               HOMEBREW_CACHE=str(evidence / 'cache'))
    records = []

    def save(name, data):
        p = evidence / name
        p.write_text(json.dumps(data, indent=2) + '\n'); p.chmod(0o600)

    def run(command, check=True):
        result = subprocess.run([str(x) for x in command], cwd=evidence, env=env,
                                capture_output=True, text=True)
        records.append(dict(command=[str(x) for x in command], exit=result.returncode,
                            stdout=result.stdout, stderr=result.stderr))
        save('commands.json', records)
        print(f'{"PASS" if result.returncode == 0 else "FAIL"}: {" ".join(str(x) for x in command[:3])} (exit {result.returncode})', flush=True)
        if check and result.returncode:
            raise RuntimeError(f'command failed; inspect {evidence / "commands.json"}')
        return result

    prefix = Path(run([brew, '--prefix']).stdout.strip())
    public_binary = prefix / 'bin/acal-native-proof'
    installed = run([brew, 'list', '--formula', '--versions', 'acal-native-proof'], check=False)
    cellar = Path(run([brew, '--cellar']).stdout.strip()) / 'acal-native-proof'
    if installed.returncode == 0 or os.path.lexists(public_binary) or cellar.exists():
        raise RuntimeError('acal-native-proof already exists; refusing to modify an existing installation')
    production = prefix / 'bin/acal'
    production_target = os.readlink(production) if production.is_symlink() else None
    production_hash = hashlib.sha256(production.read_bytes()).hexdigest() if production.is_file() else None
    tap = 'acal-proof/local-' + uuid.uuid4().hex[:12]
    formula_name = tap + '/acal-native-proof'
    created_tap = False
    snapshots = []
    before_formulae = sorted(run([brew, 'list', '--formula', '--versions']).stdout.splitlines())
    save('formulae-before.json', before_formulae)
    save('ownership.json', dict(tap=tap, formula=formula_name, preexisting_formula=False))
    try:
        run([brew, 'tap-new', '--no-git', tap]); created_tap = True
        tap_dir = Path(run([brew, '--repo', tap]).stdout.strip())
        formula = tap_dir / 'Formula/acal-native-proof.rb'
        for index, directory in enumerate([old, new]):
            formula.write_text(run([sys.executable, script_dir / 'native-proof-formula.py',
                                    '--package-dir', directory, '--revision', str(index)]).stdout)
            run([brew, 'install' if index == 0 else 'upgrade', '--formula', formula_name])
            run([brew, 'test', formula_name])
            actual = public_binary.resolve(strict=True)
            root = actual.parent.parent
            bundle = root / 'libexec/acal-native.app'
            helper = bundle / 'Contents/MacOS/acal-native'
            run(['/usr/bin/codesign', '--verify', '--strict', actual])
            run(['/usr/bin/codesign', '--verify', '--strict', bundle])
            build = json.loads((root / 'BUILD-INFO.json').read_text())
            expected = json.loads((directory / 'BUILD-INFO.json').read_text())
            if build != expected:
                raise RuntimeError('Homebrew installed different build metadata than requested')
            setup = json.loads(run([public_binary, 'native', 'setup', '--json', '--no-input']).stdout)
            if not setup.get('data', {}).get('ready'):
                raise RuntimeError('noninteractive native setup is not ready; no permission reset will be attempted')
            if index > 0:
                helper_build = setup['data'].get('helper_build', {})
                if helper_build != {'version': build['version'], 'date': build['built_at']}:
                    raise RuntimeError('helper build identity does not match the installed package')
            snapshots.append(dict(path=str(actual), build=build,
                                  helper_sha256=hashlib.sha256(helper.read_bytes()).hexdigest(), setup=setup))
            save('installed-builds.json', snapshots)
            run([sys.executable, script_dir / 'native-proof-smoke.py', '--binary', public_binary,
                 '--local', '--allow-calendar-writes'])
        assert snapshots[0]['path'] != snapshots[1]['path'], 'upgrade did not move to a new keg'
        assert snapshots[0]['helper_sha256'] != snapshots[1]['helper_sha256'], 'upgrade did not exercise changed helper bytes'
        save('result.json', dict(passed=True, scope='local file-URL tap; existing desktop-agent grant; beta host',
                                 fresh_consent='untested', public_https_delivery='untested', terminal='untested'))
    except Exception as error:
        save('result.json', dict(passed=False, error=str(error)))
        raise
    finally:
        cleanup_ok = True
        if cellar.exists():
            # Preflight proved there was no existing keg; remove only this owned formula.
            result = run([brew, 'uninstall', '--formula', '--force', formula_name], check=False)
            cleanup_ok = result.returncode == 0 and not cellar.exists() and not os.path.lexists(public_binary)
        cleanup_ok = cleanup_ok and not cellar.exists() and not os.path.lexists(public_binary)
        if created_tap and cleanup_ok:
            cleanup_ok = run([brew, 'untap', tap], check=False).returncode == 0
        after_target = os.readlink(production) if production.is_symlink() else None
        after_hash = hashlib.sha256(production.read_bytes()).hexdigest() if production.is_file() else None
        unchanged = production_target == after_target and production_hash == after_hash
        after_formulae = sorted(run([brew, 'list', '--formula', '--versions']).stdout.splitlines())
        save('formulae-after.json', after_formulae)
        formulae_unchanged = before_formulae == after_formulae
        save('cleanup.json', dict(task_formula_and_tap_removed=cleanup_ok, production_acal_unchanged=unchanged,
                                 installed_formula_versions_unchanged=formulae_unchanged))
        if not cleanup_ok or not unchanged or not formulae_unchanged:
            save('result.json', dict(passed=False, error='cleanup or installation isolation failed'))
            raise RuntimeError(f'cleanup or production isolation needs inspection: {evidence}')


if __name__ == '__main__':
    main()
