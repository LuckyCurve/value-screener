"""Tests for the OpenD client's pure parts: rate limiting, code prefixes,
period parsing, and the balance-sheet arithmetic."""

import pytest

from datalayer import futuclient


class FakeClock:
    def __init__(self):
        self.now = 0.0
        self.slept = []

    def __call__(self):
        return self.now

    def sleep(self, seconds):
        self.slept.append(seconds)
        self.now += seconds


def test_rate_limiter_lets_the_first_call_through():
    clock = FakeClock()
    RateLimiter = futuclient.RateLimiter
    limiter = RateLimiter(interval=1.0, clock=clock, sleep=clock.sleep)
    limiter.wait()
    assert clock.slept == []


def test_rate_limiter_spaces_subsequent_calls():
    clock = FakeClock()
    limiter = futuclient.RateLimiter(interval=1.0, clock=clock, sleep=clock.sleep)
    limiter.wait()
    limiter.wait()
    assert clock.slept == [pytest.approx(1.0)]


def test_rate_limiter_does_not_sleep_after_elapsed_time():
    clock = FakeClock()
    limiter = futuclient.RateLimiter(interval=1.0, clock=clock, sleep=clock.sleep)
    limiter.wait()
    clock.now = 5.0
    limiter.wait()
    assert clock.slept == []


@pytest.mark.parametrize("raw,want", [
    ("00700", "HK.00700"),
    ("600519", "SH.600519"),
    ("000651", "SZ.000651"),
    ("PEP", "US.PEP"),
    ("HK.00700", "HK.00700"),
    (" 00700 ", "HK.00700"),
])
def test_full_code_adds_market_prefix(raw, want):
    assert futuclient.full_code(raw) == want


def test_fiscal_year_reads_annual_periods():
    assert futuclient._fiscal_year({"period_text": "2024/FY"}) == 2024


def test_fiscal_year_ignores_quarters():
    assert futuclient._fiscal_year({"period_text": "2024/Q2"}) is None
    assert futuclient._fiscal_year({}) is None


def test_fiscal_year_ignores_malformed_period():
    assert futuclient._fiscal_year({"period_text": "FY/2024"}) is None


def _balance_report(year, items):
    return {
        "period_text": f"{year}/FY",
        "item_list": [
            {"display_name": name, "data": value} for name, value in items.items()
        ],
    }


def test_balance_series_sums_cash_and_takes_debt_magnitude(monkeypatch):
    report = _balance_report(2024, {
        "现金及等价物": 100.0,
        "定期存款-流动资产": 20.0,
        "银行贷款及透支": -30.0,
        "长期银行贷款": -10.0,
    })
    monkeypatch.setattr(futuclient, "_balance_report_for", lambda code, st: [report],
                        raising=False)

    client = futuclient.QuoteClient()
    monkeypatch.setattr(client, "annual_reports", lambda code, st: [report])
    got = client.balance_series("HK.00001")

    assert got == {2024: {"cash": 120.0, "debt": 40.0}}


def test_balance_series_ignores_lines_outside_the_mapping(monkeypatch):
    report = _balance_report(2024, {
        "现金及等价物": 100.0,
        "应付账款": -999.0,  # trade payable: not interest-bearing
    })
    client = futuclient.QuoteClient()
    monkeypatch.setattr(client, "annual_reports", lambda code, st: [report])
    got = client.balance_series("HK.00001")
    assert got[2024]["debt"] == 0.0


def test_balance_series_treats_missing_lines_as_zero(monkeypatch):
    report = _balance_report(2024, {"现金及等价物": 50.0})
    client = futuclient.QuoteClient()
    monkeypatch.setattr(client, "annual_reports", lambda code, st: [report])
    got = client.balance_series("HK.00001")
    assert got[2024] == {"cash": 50.0, "debt": 0.0}


def test_balance_series_unknown_exchange_raises(monkeypatch):
    client = futuclient.QuoteClient()
    monkeypatch.setattr(client, "annual_reports", lambda code, st: [])
    with pytest.raises(ValueError, match="no balance-sheet mapping"):
        client.balance_series("JP.7203")


def test_roe_series_reads_annual_reports_only(monkeypatch):
    reports = [
        _balance_report(2023, {"净资产收益率(ROE)": 25.0}),
        {"period_text": "2024/Q2", "item_list": [
            {"display_name": "净资产收益率(ROE)", "data": 99.0}]},
        _balance_report(2024, {"净资产收益率(ROE)": 30.0}),
    ]
    client = futuclient.QuoteClient()
    monkeypatch.setattr(client, "annual_reports", lambda code, st: reports)
    assert client.roe_series("HK.00001") == {2023: 0.25, 2024: 0.30}


def test_roe_series_skips_years_without_a_figure(monkeypatch):
    reports = [_balance_report(2024, {"净资产收益率(ROE)": None})]
    client = futuclient.QuoteClient()
    monkeypatch.setattr(client, "annual_reports", lambda code, st: reports)
    assert client.roe_series("HK.00001") == {}


def test_valuation_rejects_unsupported_window(monkeypatch):
    client = futuclient.QuoteClient()
    with pytest.raises(ValueError, match="no valuation window"):
        client.valuation("HK.00001", 7)


def test_valuation_maps_trend_fields(monkeypatch):
    seen = {}

    def fake_call(ctx, code, interval):
        seen["interval"] = interval
        return futuclient.RET_OK, {"trend": {
            "valuation_percentile": 12.5,
            "average_value": 20.0,
            "current_value": 15.0,
            "forward_value": 14.0,
        }}

    monkeypatch.setattr(futuclient, "_valuation_call", fake_call)
    monkeypatch.setattr(futuclient, "_valuation_interval", lambda years: f"y{years}")
    client = futuclient.QuoteClient()
    client._ctx = object()
    got = client.valuation("HK.00001", 10)

    assert got == {"percentile": 12.5, "average": 20.0, "current": 15.0,
                   "forward": 14.0}
    assert seen["interval"] == "y10"


def test_rate_limit_messages_are_recognised():
    assert futuclient._is_rate_limit("获取财务报表频率太高，请求失败，每30秒最多30次。")
    assert futuclient._is_rate_limit("Too many requests")
    assert not futuclient._is_rate_limit("暂不提供美股 OTC 市场行情")


def test_call_retries_on_rate_limit_then_succeeds(monkeypatch):
    calls = {"n": 0}

    class Ret:
        def __init__(self, ok):
            self.ok = ok
            self.msg = 0 if ok else "频率太高"

    def fn():
        calls["n"] += 1
        if calls["n"] < 2:
            return -1, "获取财务报表频率太高"
        return 0, {"ok": True}

    monkeypatch.setattr(futuclient.time, "sleep", lambda s: None)
    client = futuclient.QuoteClient(limiter=FakeLimiter(), max_retries=3)
    got = client._call(fn)
    assert got == {"ok": True}
    assert calls["n"] == 2


class FakeLimiter:
    def wait(self):
        pass
