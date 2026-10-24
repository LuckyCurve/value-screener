"""Progress lines must be one JSON object per line, so the app can read them."""

import io
import json

from datalayer.progress import Progress


def lines(stream):
    return [json.loads(line) for line in stream.getvalue().splitlines()]


def test_phase_event():
    s = io.StringIO()
    Progress(s).phase("first pass")
    assert lines(s) == [{"event": "phase", "name": "first pass"}]


def test_stock_event_carries_position():
    s = io.StringIO()
    Progress(s).stock(2, 10, "HK.00700", "Tencent")
    assert lines(s) == [{
        "event": "stock", "index": 2, "total": 10,
        "code": "HK.00700", "name": "Tencent",
    }]


def test_each_call_is_one_line():
    s = io.StringIO()
    p = Progress(s)
    p.phase("x")
    p.found(3)
    p.done(3)
    out = lines(s)
    assert [e["event"] for e in out] == ["phase", "found", "done"]
    assert len(s.getvalue().splitlines()) == 3


def test_unicode_names_survive():
    s = io.StringIO()
    Progress(s).stock(1, 1, "SH.600519", "贵州茅台")
    assert "贵州茅台" in s.getvalue()


def test_bytes_are_utf8_even_when_the_stream_has_another_code_page():
    """Windows gives a piped stderr the console's code page. Writing the line
    as text put GBK bytes on the wire and the app, which reads UTF-8, showed
    a stock's Chinese name as mojibake."""

    class PipedStderr(io.TextIOWrapper):
        """What sys.stderr is on Windows: a text stream over bytes."""

        def __init__(self):
            self.buf = io.BytesIO()
            super().__init__(self.buf, encoding="gbk")

    s = PipedStderr()
    Progress(s).stock(1, 1, "SH.600519", "贵州茅台")
    raw = s.buf.getvalue()

    # The bytes are UTF-8, not the stream's own code page.
    assert "贵州茅台".encode("utf-8") in raw
    assert json.loads(raw.decode("utf-8"))["name"] == "贵州茅台"
