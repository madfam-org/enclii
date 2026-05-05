"""Make `import check_main_ci` resolve from the parent directory."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
