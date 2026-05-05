"""Make probe.py importable from tests/ without packaging gymnastics."""

from __future__ import annotations

import sys
from pathlib import Path

# Tests live at infra/synthetic-flow-probe/tests/, probe.py lives one level up.
HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
