"""Progress lines the app reads while a run is in flight.

Written to stderr as one JSON object per line, so the Go side can show a
progress bar without parsing prose.
"""

from __future__ import annotations

import json
import sys


class Progress:
    def __init__(self, stream=None):
        self._stream = stream if stream is not None else sys.stderr

    def _emit(self, **payload) -> None:
        self._stream.write(json.dumps(payload, ensure_ascii=False) + "\n")
        self._stream.flush()

    def phase(self, name: str) -> None:
        self._emit(event="phase", name=name)

    def found(self, count: int) -> None:
        self._emit(event="found", count=count)

    def stock(self, index: int, total: int, code: str, name: str) -> None:
        self._emit(event="stock", index=index, total=total, code=code, name=name)

    def error(self, message: str) -> None:
        self._emit(event="error", message=message)

    def done(self, count: int) -> None:
        self._emit(event="done", count=count)
