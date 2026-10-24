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
        self._write(json.dumps(payload, ensure_ascii=False) + "\n")

    def _write(self, line: str) -> None:
        """Write the line as UTF-8 bytes, whatever the stream's encoding is.

        Windows gives a piped stderr the console's code page, so writing a
        stock's Chinese name as text put GBK bytes on the wire and the app,
        which reads UTF-8, showed them as mojibake. Text has to go to the
        underlying byte buffer: handing bytes to a text stream re-encodes
        them with the stream's own code page, which is the bug itself.
        """
        stream = self._stream
        buf = getattr(stream, "buffer", None)
        if buf is not None:
            buf.write(line.encode("utf-8"))
            buf.flush()
            return
        # A test's StringIO, or any stream that takes text.
        stream.write(line)
        stream.flush()

    def phase(self, name: str) -> None:
        self._emit(event="phase", name=name)

    def found(self, count: int) -> None:
        self._emit(event="found", count=count)

    def stock(self, index: int, total: int, code: str, name: str) -> None:
        self._emit(event="stock", index=index, total=total, code=code, name=name)

    def candidate(self, stocks: list[dict]) -> None:
        """One stock's first-pass figures, so the window can show it at once.

        The same list is in the packet at the end; this is for the table
        above the run, not a second source of truth.
        """
        self._emit(event="candidate", stocks=stocks)

    def judged(self, stock: dict) -> None:
        """One stock's folded figures, ready to be judged.

        Written after the stock's record is in the packet, so a reader that
        takes this gives up nothing if the run is killed next.
        """
        self._emit(event="judged", stock=stock)

    def error(self, message: str) -> None:
        self._emit(event="error", message=message)

    def done(self, count: int) -> None:
        self._emit(event="done", count=count)


def fold(entry: dict) -> dict:
    """The year keys the packet writes as strings, back to the numbers the
    judgement reads."""
    return {
        "code": entry.get("code", ""),
        "name": entry.get("name", ""),
        "roe": {int(y): v for y, v in (entry.get("roe") or {}).items()},
        "balance": {
            int(y): {"cash": b.get("cash", 0.0), "debt": b.get("debt", 0.0)}
            for y, b in (entry.get("balance") or {}).items()
        },
        "valuation": entry.get("valuation"),
        "error": entry.get("error", ""),
    }
