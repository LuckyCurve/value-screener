"""Reading the criteria file on the Python side.

The Go engine owns validation. This reader mirrors the defaults so a run
without a config file behaves the same as the app's, and refuses values that
would make the data fetch meaningless.
"""

from __future__ import annotations

import json
from pathlib import Path

DEFAULTS = {
    "markets": ["HK", "US", "CN"],
    "pe_max": 20.0,
    "roe_min": 0.20,
    "roe_years": 15,
    "roe_slack": 0,
    "coarse_roe_min": 0.15,
    "cash_years": 10,
    "percentile_years": 10,
}

VALID_MARKETS = {"HK", "US", "CN"}
# The valuation endpoint only offers these windows.
VALID_PERCENTILE_YEARS = {1, 3, 5, 10}


def load_criteria(path: str | None) -> dict:
    """Load criteria, defaulting anything absent."""
    criteria = dict(DEFAULTS)
    if path:
        file = Path(path)
        if file.exists():
            criteria.update(json.loads(file.read_text(encoding="utf-8")))
    validate(criteria)
    return criteria


def validate(criteria: dict) -> None:
    """Raise ValueError on values the fetch cannot honour.

    Keys left out fall back to the defaults, matching how load_criteria
    merges a partial file.
    """
    merged = dict(DEFAULTS)
    merged.update(criteria)
    criteria = merged

    markets = criteria.get("markets") or []
    if not markets:
        raise ValueError("markets must not be empty")
    unknown = set(markets) - VALID_MARKETS
    if unknown:
        raise ValueError(f"unknown markets {sorted(unknown)}; want {sorted(VALID_MARKETS)}")

    pe_max = criteria.get("pe_max")
    if pe_max is None or pe_max <= 0:
        raise ValueError(f"pe_max must be positive, got {pe_max}")

    for name in ("roe_min", "coarse_roe_min"):
        value = criteria.get(name)
        if value is None or value <= 0:
            raise ValueError(f"{name} must be positive, got {value}")

    years = criteria.get("percentile_years")
    if years not in (0, *VALID_PERCENTILE_YEARS):
        raise ValueError(
            f"percentile_years must be 0 to skip, or one of "
            f"{sorted(VALID_PERCENTILE_YEARS)}; got {years}"
        )
