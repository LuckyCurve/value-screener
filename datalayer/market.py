"""Per-market field names in Futu's financial statements.

The same figure carries a different label in each market: Hong Kong uses
half-width brackets, the US full-width, and A-shares report a weighted
published value. Reading the wrong name yields None, which the engine would
then judge as a missed year — a silent wrong answer rather than an error.
Every name here was checked against a live response.
"""

from __future__ import annotations

from dataclasses import dataclass

# ROE label candidates, tried in order. A stock reports at most one.
ROE_FIELDS = (
    "净资产收益率(ROE)",  # HK
    "净资产收益率（ROE）",  # US, full-width brackets
    "净资产收益率_加权公布值",  # A-shares
)

# Cash lines: counted as cash and equivalents.
CASH_HK = ("现金及等价物", "定期存款-流动资产", "定期存款-非流动资产")
CASH_US = ("-现金和现金等价物", "-短期投资")
CASH_CN = ("货币资金", "交易性金融资产")

# Interest-bearing debt lines. Exchanges disagree on whether a liability
# comes back positive or negative, so the caller takes each line's magnitude:
# a borrowing is never negative, and a sign taken from the wrong convention
# would make the net-cash test pass everything.
DEBT_HK = (
    "银行贷款及透支",
    "短期融资租赁负债",
    "长期银行贷款",
    "长期融资租赁负债",
    "可转换票据及债券",
)
DEBT_US = ("-短期借款", "-短期融资租赁负债", "-长期借款", "-长期融资租赁负债")
DEBT_CN = ("短期借款", "一年内到期的非流动负债", "长期借款", "租赁负债")


@dataclass(frozen=True)
class SheetNames:
    """Balance-sheet line names for one exchange."""

    cash: tuple[str, ...]
    debt: tuple[str, ...]


# A-shares split across two exchanges that share one set of line names.
SHEETS = {
    "HK": SheetNames(CASH_HK, DEBT_HK),
    "US": SheetNames(CASH_US, DEBT_US),
    "SH": SheetNames(CASH_CN, DEBT_CN),
    "SZ": SheetNames(CASH_CN, DEBT_CN),
    "CN": SheetNames(CASH_CN, DEBT_CN),
}


def exchange_of(code: str) -> str:
    """Return the exchange prefix of a Futu code, e.g. 'SH.600519' -> 'SH'."""
    return code.split(".", 1)[0].upper()


def sheet_for(code: str) -> SheetNames:
    exchange = exchange_of(code)
    try:
        return SHEETS[exchange]
    except KeyError:
        raise ValueError(
            f"no balance-sheet mapping for exchange {exchange!r} in {code!r}; "
            f"add one to market.py before screening this market"
        ) from None


def roe_of(items: dict[str, float | None]) -> float | None:
    """Read ROE from one annual report, as a fraction. Futu reports percent."""
    for name in ROE_FIELDS:
        value = items.get(name)
        if value is not None:
            return value / 100.0
    return None


def field_names() -> dict[str, list[str]]:
    """Expose the mapping for diagnostics and documentation."""
    return {
        "roe": list(ROE_FIELDS),
        "cash": {
            "HK": list(CASH_HK),
            "US": list(CASH_US),
            "CN": list(CASH_CN),
        },
        "debt": {
            "HK": list(DEBT_HK),
            "US": list(DEBT_US),
            "CN": list(DEBT_CN),
        },
    }
