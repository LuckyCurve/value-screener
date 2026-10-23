"""Tests for criteria loading and checking on the data side."""

import json

import pytest

from datalayer import config_io


def write(tmp_path, payload):
    path = tmp_path / "criteria.json"
    path.write_text(json.dumps(payload), encoding="utf-8")
    return str(path)


def test_missing_path_yields_defaults():
    assert config_io.load_criteria(None)["pe_max"] == 20.0


def test_absent_file_yields_defaults(tmp_path):
    got = config_io.load_criteria(str(tmp_path / "nope.json"))
    assert got["markets"] == ["HK", "US", "CN"]


def test_file_overrides_only_named_keys(tmp_path):
    got = config_io.load_criteria(write(tmp_path, {"pe_max": 15, "roe_min": 0.25}))
    assert got["pe_max"] == 15
    assert got["roe_min"] == 0.25
    assert got["cash_years"] == 10


def test_empty_markets_rejected():
    with pytest.raises(ValueError, match="markets must not be empty"):
        config_io.validate({"markets": [], "pe_max": 20})


def test_unknown_market_rejected():
    with pytest.raises(ValueError, match="unknown markets"):
        config_io.validate({"markets": ["JP"], "pe_max": 20})


def test_non_positive_pe_rejected():
    with pytest.raises(ValueError, match="pe_max must be positive"):
        config_io.validate({"markets": ["HK"], "pe_max": 0})


def test_non_positive_roe_rejected():
    with pytest.raises(ValueError, match="roe_min must be positive"):
        config_io.validate({"markets": ["HK"], "pe_max": 20, "roe_min": -1})


def test_unsupported_percentile_window_rejected():
    with pytest.raises(ValueError, match="percentile_years"):
        config_io.validate(
            {"markets": ["HK"], "pe_max": 20, "percentile_years": 7})


def test_zero_percentile_window_skips_valuation():
    config_io.validate({"markets": ["HK"], "pe_max": 20, "percentile_years": 0})
