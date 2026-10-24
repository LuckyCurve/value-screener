"""Tests for the command line: what the first pass asks for, and what a
cancelled run leaves behind."""

import io
import json
import os
import sys

from datalayer import __main__ as cli
from datalayer import futuclient
from datalayer.progress import Progress


def criteria(**over):
    c = {"markets": ["HK"], "pe_max": 20.0, "roe_min": 0.30,
         "roe_years": 15, "roe_slack": 0, "cash_years": 10,
         "percentile_years": 10}
    c.update(over)
    return c


# -- the first pass --------------------------------------------------------


def test_prefilter_uses_the_users_own_floor():
    # The floor the window shows is the floor the first pass screens on.
    floor, years = cli.prefilter(criteria())
    assert floor == 0.30


def test_prefilter_window_is_capped_at_twelve_years():
    # Futu returns nothing at all for a longer window, which used to look
    # like a market with no candidates.
    assert cli.prefilter(criteria(roe_years=30))[1] == cli.MAX_CONTINUOUS_YEARS


def test_prefilter_drops_the_roe_condition_when_slack_is_allowed():
    # A stock may then miss any year in the window, which the endpoint's
    # consecutive-year window cannot express.
    assert cli.prefilter(criteria(roe_slack=2)) == (None, 0)


def test_first_pass_without_a_floor_asks_for_no_roe_condition(monkeypatch):
    seen = {}

    class FakeRequest:
        def add_simple_field(self, *a):
            pass

        def add_simple_property(self, *a, **k):
            pass

        def add_financial_property(self, *a, **k):
            seen["roe"] = True

        def add_retrieve_basic(self, *a):
            pass

        def add_retrieve_simple(self, *a):
            pass

    # The SDK is absent on CI, so the two imports first_pass makes are
    # answered from sys.modules.
    screen = type("M", (), {
        "StockScreenRequest": FakeRequest,
        "FinancialProperty": type("F", (), {"ROE": 1}),
    })()
    constants = type("M", (), {
        "FinancialProperty": type("F", (), {"ROE": 1}),
        "ScrMarket": type("S", (), {"HK": 1, "US": 2, "CN": 3}),
        "SimpleField": type("F", (), {"MARKET": 1}),
        "SimpleProperty": type("P", (), {"PE_TTM": 1, "PB": 2, "MARKET_CAP": 3}),
        "Term": type("T", (), {"ANNUAL": 1}),
    })()

    def fake_call(ctx, req):
        # The SDK's (ret, data) pair; data is (last_page, total, items).
        return futuclient.RET_OK, (True, 0, [])

    monkeypatch.setattr(futuclient, "_screen_call", fake_call)
    monkeypatch.setitem(sys.modules, "futu", screen)
    monkeypatch.setitem(sys.modules, "futu.quote", type("M", (), {})())
    monkeypatch.setitem(sys.modules, "futu.quote.stock_screen_const", constants)

    client = futuclient.QuoteClient()
    client.first_pass(["HK"], 20, None, 0)
    assert "roe" not in seen


# -- writing the packet ----------------------------------------------------


def test_packet_is_written_after_every_stock(tmp_path):
    """A cancelled run is killed outright, so the file must already hold the
    stocks it finished. Reading it mid-run is the only way to know."""
    out = tmp_path / "figures.json"
    after_each = []

    class Watching(futuclient.QuoteClient):
        def roe_series(self, code):
            # What the file holds before this stock is added.
            if out.exists():
                packet = json.loads(out.read_text(encoding="utf-8"))
                after_each.append([s["code"] for s in packet["stocks"]])
            else:
                after_each.append([])
            return {2024: 0.31}

        def balance_series(self, code):
            return {2024: {"cash": 10.0, "debt": 1.0}}

    args = type("A", (), {"code": ["HK.00700", "HK.00005", "HK.00011"],
                          "code_file": None, "out": str(out), "limit": 0})()
    cli._figures(Watching(), criteria(percentile_years=0), args,
                 Progress(io.StringIO()))

    assert after_each == [[], ["HK.00700"], ["HK.00700", "HK.00005"]]
    got = json.loads(out.read_text(encoding="utf-8"))
    assert len(got["stocks"]) == 3


def test_a_cancelled_run_keeps_the_stocks_it_finished(tmp_path):
    out = tmp_path / "figures.json"
    seen = []

    class Slow(futuclient.QuoteClient):
        def roe_series(self, code):
            seen.append(code)
            if len(seen) == 2:
                cli._stopping = True  # the app's stop arrives here
            return {2024: 0.31}

        def balance_series(self, code):
            return {2024: {"cash": 10.0, "debt": 1.0}}

    args = type("A", (), {"code": ["HK.00700", "HK.00005", "HK.00011"],
                          "code_file": None, "out": str(out), "limit": 0})()
    try:
        cli._figures(Slow(), criteria(percentile_years=0), args,
                     Progress(io.StringIO()))
    finally:
        cli._stopping = False

    got = json.loads(out.read_text(encoding="utf-8"))
    assert [s["code"] for s in got["stocks"]] == ["HK.00700", "HK.00005"]


def test_one_bad_stock_does_not_cost_the_rest(tmp_path):
    """An SDK error that is not OpenDError used to take the run down with
    it, losing every stock after the bad one."""
    out = tmp_path / "figures.json"

    class Flaky(futuclient.QuoteClient):
        def roe_series(self, code):
            if code == "HK.00005":
                raise KeyError("unexpected shape")
            return {2024: 0.31}

        def balance_series(self, code):
            return {2024: {"cash": 10.0, "debt": 1.0}}

    args = type("A", (), {"code": ["HK.00700", "HK.00005", "HK.00011"],
                          "code_file": None, "out": str(out), "limit": 0})()
    cli._figures(Flaky(), criteria(percentile_years=0), args,
                 Progress(io.StringIO()))

    got = json.loads(out.read_text(encoding="utf-8"))
    codes = [s["code"] for s in got["stocks"]]
    assert codes == ["HK.00700", "HK.00005", "HK.00011"]
    assert "unexpected shape" in got["stocks"][1]["error"]


def test_each_finished_stock_is_reported_before_the_run_ends(tmp_path):
    """The window fills its table from these lines; a run that only spoke
    at the end would leave it blank while it fetched."""
    out = tmp_path / "figures.json"
    stream = io.StringIO()

    class Fast(futuclient.QuoteClient):
        def roe_series(self, code):
            return {2024: 0.31}

        def balance_series(self, code):
            return {2024: {"cash": 10.0, "debt": 1.0}}

    args = type("A", (), {"code": ["HK.00700", "HK.00005"],
                          "code_file": None, "out": str(out), "limit": 0})()
    cli._figures(Fast(), criteria(percentile_years=0), args, Progress(stream))

    lines = [json.loads(line) for line in stream.getvalue().splitlines()]
    judged = [line for line in lines if line["event"] == "judged"]
    assert [line["stock"]["code"] for line in judged] == ["HK.00700", "HK.00005"]
    # The line holds the stock's own figures, not a reference to a packet.
    assert judged[0]["stock"]["roe"] == {"2024": 0.31}
    assert judged[0]["stock"]["balance"] == {
        "2024": {"cash": 10.0, "debt": 1.0}}
    # The last judged line comes after the packet already holds that stock.
    assert [s["code"] for s in json.loads(out.read_text(encoding="utf-8"))["stocks"]] == [
        "HK.00700", "HK.00005"]


def test_a_half_written_packet_is_never_left_behind(tmp_path):
    """The reader is the app, after a kill: a truncated file would read as
    no run at all."""
    out = tmp_path / "figures.json"
    save = cli._saver(str(out), criteria(), [{"code": "HK.00700"}])
    save()
    save()
    leftovers = [p for p in os.listdir(tmp_path) if p.startswith(".packet-")]
    assert leftovers == []
    assert json.loads(out.read_text(encoding="utf-8"))["stocks"][0]["code"] == "HK.00700"
