#!/usr/bin/env python3
"""Regression fixtures against the CLI's constructed metadata, without command execution."""
import importlib.util
from pathlib import Path
import shlex
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('readme_check', Path(__file__).with_name('readme-command-check.py'))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


class DocsCommandContractTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.inventory = checker.load_inventory()
        cls.manifest = (checker.ROOT / 'scripts/help-snapshots.txt').read_text()
        cls.help_dir = checker.ROOT / 'docs/help'

    def test_unknown_nested_command_rejected(self):
        for command in ('acal events definitely-not-a-command', 'acal native events definitely-not-a-command',
                        'acal --json events definitely-not-a-command'):
            with self.subTest(command=command), self.assertRaisesRegex(ValueError, 'unknown subcommand'):
                checker.check_tokens(shlex.split(command), self.inventory)

    def test_unknown_flags_rejected(self):
        for command in ('acal events add --definitely-not-a-flag', 'acal events update id --definitely-not-a-flag=1',
                        'acal events list -Z', 'acal events --title WrongPlace'):
            with self.subTest(command=command), self.assertRaisesRegex(ValueError, 'unknown flag'):
                checker.check_tokens(shlex.split(command), self.inventory)

    def test_flag_values_required(self):
        for command in ('acal events add --title', 'acal events add --title --json',
                        'acal --profile', 'acal events list --json=invalid'):
            with self.subTest(command=command), self.assertRaises(ValueError):
                checker.check_tokens(shlex.split(command), self.inventory)

    def test_leaf_operands_and_flags_accepted(self):
        for command in ('acal events update definitely-not-a-command --title Changed --dry-run --json',
                        'acal --json events delete id --confirm id --no-input',
                        'acal events update id --all-day=false --dry-run',
                        'acal events search "a title with spaces" -qv --from -7d',
                        'acal events search -- --not-a-flag', 'acal queries save name --where "title~a,b"'):
            with self.subTest(command=command):
                checker.check_tokens(shlex.split(command), self.inventory)

    def test_readme_faults_rejected(self):
        readme = (checker.ROOT / 'README.md').read_text()
        self.assertGreater(checker.check_readme(readme, self.inventory), 0)
        for fault in ('acal events definitely-not-a-command', 'acal events add --definitely-not-a-flag'):
            with self.subTest(fault=fault), self.assertRaises(ValueError):
                checker.check_readme(readme + '\n' + fault + '\n', self.inventory)

    def test_manifest_matches_command_tree(self):
        self.assertEqual(checker.check_snapshots(self.manifest, self.inventory, self.help_dir), len(self.inventory))

    def test_missing_and_duplicate_manifest_paths_rejected(self):
        lines = self.manifest.splitlines()
        entry = next(line for line in lines if not line.startswith('#'))
        for changed in ('\n'.join(line for line in lines if line != entry), self.manifest + entry + '\n'):
            with self.assertRaises(ValueError):
                checker.check_snapshots(changed, self.inventory, self.help_dir)

    def test_unknown_manifest_command_rejected(self):
        with self.assertRaisesRegex(ValueError, 'unknown or duplicate'):
            checker.check_snapshots(self.manifest + 'bad.txt\tevents bad --help\n', self.inventory, self.help_dir)

    def test_missing_snapshot_file_rejected(self):
        with tempfile.TemporaryDirectory() as work, self.assertRaisesRegex(ValueError, 'missing help snapshot'):
            checker.check_snapshots(self.manifest, self.inventory, Path(work))


if __name__ == '__main__':
    unittest.main()
