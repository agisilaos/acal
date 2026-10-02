#!/usr/bin/env python3
"""Run the pinned common helper from this consumer repository."""
import os
from pathlib import Path
import sys

root = Path(__file__).resolve().parents[1]
os.chdir(root)
helper = root / 'scripts/cli-shared' / Path(__file__).name
os.execv(sys.executable, [sys.executable, str(helper), *sys.argv[1:]])
