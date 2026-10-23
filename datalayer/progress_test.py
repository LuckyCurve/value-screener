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
