"""Entry point PyInstaller freezes into value-screener-dl.

The app runs this binary exactly as it would run `python -m datalayer`, so the
build changes how the data layer is invoked and nothing about what it does.
"""

import sys

from datalayer.__main__ import main

if __name__ == "__main__":
    sys.exit(main())
