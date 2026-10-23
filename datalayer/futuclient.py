"""Thin wrapper over the Futu quote context.

Only the quote context is ever used: this module never opens a trade context,
never unlocks trading, and never places or cancels an order. The data layer
reads; it does not transact.

The Futu SDK is imported lazily so the pure mapping and formatting tests run
without it installed.
"""

from __future__ import annotations

import time
from typing import Any, Iterator

from .market import field_names as _field_names
from .market import roe_of, sheet_for

# The financial-statement endpoint allows 30 calls per 30 seconds. Staying a
# little under that keeps a long run from tripping the limit.
MIN_CALL_INTERVAL = 1.25

# The SDK's success code. Mirrored here so the retry logic can be tested
# without the futu package installed.
RET_OK = 0


class RateLimiter:
    """Space calls out so a long run never trips the endpoint's rate limit."""

    def __init__(self, interval: float = MIN_CALL_INTERVAL, clock=time.monotonic,
                 sleep=time.sleep):
        self.interval = interval
        self._clock = clock
        self._sleep = sleep
        self._next_at = 0.0

    def wait(self) -> None:
        now = self._clock()
        if now < self._next_at:
            self._sleep(self._next_at - now)
        self._next_at = self._clock() + self.interval


class OpenDError(RuntimeError):
    """OpenD is unreachable or refused a request for a non-retryable reason."""


def _is_rate_limit(message: str) -> bool:
    text = message.lower()
    markers = ("频繁", "频率", "frequent", "too many", "rate limit", "throttl")
    return any(marker in text for marker in markers)


class QuoteClient:
    """Read-only access to the Futu quote API."""

    def __init__(self, host: str = "127.0.0.1", port: int = 11111,
                 limiter: RateLimiter | None = None, max_retries: int = 4):
        self.host = host
        self.port = port
        self.limiter = limiter or RateLimiter()
        self.max_retries = max_retries
        self._ctx: Any = None

    # -- lifecycle ---------------------------------------------------------

    def connect(self):
        if self._ctx is not None:
            return self._ctx
        try:
            from futu import OpenQuoteContext
        except ImportError as exc:  # pragma: no cover - environment dependent
            raise OpenDError(
                "the futu-api package is not installed; run "
                "`pip install futu-api` in the interpreter the app uses"
            ) from exc
        self._ctx = OpenQuoteContext(host=self.host, port=self.port)
        return self._ctx

    def close(self) -> None:
        if self._ctx is not None:
            try:
                self._ctx.close()
            finally:
                self._ctx = None

    def __enter__(self):
        self.connect()
        return self

    def __exit__(self, *exc_info):
        self.close()

    # -- calls -------------------------------------------------------------

    def _call(self, fn, *args, **kwargs):
        last = ""
        for attempt in range(self.max_retries + 1):
            self.limiter.wait()
            ret, data = fn(*args, **kwargs)
            if ret == RET_OK:
                return data
            last = str(data)
            if not _is_rate_limit(last):
                raise OpenDError(last)
            if attempt < self.max_retries:
                time.sleep(10 * (attempt + 1))
        raise OpenDError(f"rate limited after {self.max_retries} retries: {last}")

    def first_pass(self, markets: list[str], pe_max: float, roe_min: float,
                   continuous_years: int) -> list[dict]:
        """Return codes whose PE and recent ROE history clear the prefilter.

        The prefilter is deliberately loose: it only needs to avoid missing a
        stock the per-stock pass would keep. The engine makes the decision.
        """
        from futu import StockScreenRequest
        from futu.quote.stock_screen_const import (
            FinancialProperty,
            ScrMarket,
            SimpleField,
            SimpleProperty,
            Term,
        )

        market_ids = [ScrMarket.HK, ScrMarket.US, ScrMarket.CN]
        wanted = {m.upper() for m in markets}
        allowed = [
            mid for mid, name in ((ScrMarket.HK, "HK"), (ScrMarket.US, "US"),
                                  (ScrMarket.CN, "CN"))
            if name in wanted
        ]
        if not allowed:
            return []

        req = StockScreenRequest()
        req.add_simple_field(SimpleField.MARKET, allowed)
        req.add_simple_property(SimpleProperty.PE_TTM, lower=0, upper=pe_max)
        req.add_financial_property(
            FinancialProperty.ROE, term=Term.ANNUAL,
            lower=roe_min, continuous_period=max(1, min(12, continuous_years)),
        )
        req.add_retrieve_basic(1101)
        req.add_retrieve_basic(1102)
        req.add_retrieve_simple(SimpleProperty.PE_TTM)
        req.add_retrieve_simple(SimpleProperty.PB)
        req.add_retrieve_simple(SimpleProperty.MARKET_CAP)

        out: list[dict] = []
        page_from = 0
        while True:
            req.page_from = page_from
            req.page_count = 200
            data = self._call(_screen_call, self._ctx, req)
            last_page, _total, items = data
            for item in items:
                rec: dict[str, Any] = {}
                for row in item["results"]:
                    name = row["property"]["name"]
                    value = row.get("sval")
                    if value is None:
                        value = row.get("dval")
                    if value is None:
                        value = row.get("ival")
                    rec[name] = value
                code = str(rec.get(1101) or "")
                out.append({
                    "code": full_code(code),
                    "name": rec.get(1102) or "",
                    "pe": float(rec.get(SimpleProperty.PE_TTM) or 0),
                    "pb": float(rec.get(SimpleProperty.PB) or 0),
                    "market_cap": float(rec.get(SimpleProperty.MARKET_CAP) or 0),
                })
            if last_page or not items:
                break
            page_from += len(items)
        return out

    def annual_reports(self, code: str, statement_type: int,
                       financial_type: int = 7) -> list[dict]:
        """Fetch every annual report of one statement type."""
        out: list[dict] = []
        next_key = None
        for _ in range(20):
            data = self._call(
                _financials_call, self._ctx, code, statement_type,
                financial_type, next_key,
            )
            out.extend(data["report_list"])
            next_key = data.get("next_key")
            if not next_key or next_key == "-1":
                break
        return out

    # -- derived -----------------------------------------------------------

    def roe_series(self, code: str) -> dict[int, float]:
        """Annual ROE by fiscal year, as fractions."""
        series: dict[int, float] = {}
        for rep in self.annual_reports(code, _statement_key_metrics()):
            year = _fiscal_year(rep)
            if year is None:
                continue
            value = roe_of(_items(rep))
            if value is not None:
                series[year] = value
        return series

    def balance_series(self, code: str) -> dict[int, dict[str, float]]:
        """Cash and interest-bearing debt by fiscal year."""
        names = sheet_for(code)
        series: dict[int, dict[str, float]] = {}
        for rep in self.annual_reports(code, _statement_balance()):
            year = _fiscal_year(rep)
            if year is None:
                continue
            items = _items(rep)
            cash = sum((items.get(n) or 0.0) for n in names.cash)
            # Debt lines are reported negative; take the magnitude.
            debt = -sum((items.get(n) or 0.0) for n in names.debt)
            series[year] = {"cash": cash, "debt": debt}
        return series

    def valuation(self, code: str, years: int) -> dict | None:
        """Current PE and its percentile over the requested window."""
        supported = (1, 3, 5, 10)
        if years not in supported:
            raise ValueError(
                f"no valuation window for {years} years; choose one of "
                f"{list(supported)}"
            )
        data = self._call(_valuation_call, self._ctx, code, _valuation_interval(years))
        trend = data.get("trend") or {}
        return {
            "percentile": float(trend.get("valuation_percentile") or 0),
            "average": float(trend.get("average_value") or 0),
            "current": float(trend.get("current_value") or 0),
            "forward": float(trend.get("forward_value") or 0),
        }


# -- module-level shims ----------------------------------------------------
# Kept separate so tests can substitute them without importing futu.


def _screen_call(ctx, req):
    return ctx.get_stock_screen(req)


def _financials_call(ctx, code, statement_type, financial_type, next_key):
    return ctx.get_financials_statements(
        code=code,
        statement_type=statement_type,
        financial_type=financial_type,
        next_key=next_key,
        num=50,
    )


def _valuation_call(ctx, code, interval):
    # Returns the SDK's (ret, data) pair, as every other shim does: _call
    # owns the retry and the error.
    return ctx.get_valuation_detail(code, interval_type=interval)


def _valuation_interval(years: int):
    """Map a window in years to the SDK's interval enum."""
    from futu import ValuationIntervalType

    return {
        1: ValuationIntervalType.YEAR_1,
        3: ValuationIntervalType.YEAR_3,
        5: ValuationIntervalType.YEAR_5,
        10: ValuationIntervalType.YEAR_10,
    }[years]


def _statement_key_metrics() -> int:
    return 4


def _statement_balance() -> int:
    return 2


def _items(report: dict) -> dict[str, float | None]:
    return {i["display_name"]: i.get("data") for i in report["item_list"]}


def _fiscal_year(report: dict) -> int | None:
    period = report.get("period_text") or ""
    if not period.endswith("/FY"):
        return None
    try:
        return int(period.split("/")[0])
    except ValueError:
        return None


def full_code(code: str) -> str:
    """Add the market prefix the screen endpoint omits."""
    code = code.strip()
    if "." in code:
        return code
    if code.isdigit():
        if len(code) == 5:
            return f"HK.{code}"
        if len(code) == 6:
            return f"SH.{code}" if code[0] == "6" else f"SZ.{code}"
    return f"US.{code}"
