"""Tests for the per-market field mapping."""

import pytest

from datalayer.market import (
    exchange_of,
    roe_of,
    sheet_for,
    field_names,
)


def test_exchange_of_splits_on_dot():
    assert exchange_of("HK.00700") == "HK"
    assert exchange_of("US.PEP") == "US"
    assert exchange_of("SH.600519") == "SH"
    assert exchange_of("SZ.000651") == "SZ"


def test_exchange_of_is_case_insensitive():
    assert exchange_of("hk.00700") == "HK"


def test_sheet_for_each_supported_exchange():
    for code in ("HK.00700", "US.PEP", "SH.600519", "SZ.000651"):
        assert sheet_for(code).cash
        assert sheet_for(code).debt


def test_sh_and_sz_share_a_share_line_names():
    assert sheet_for("SH.600519") == sheet_for("SZ.000651")


def test_sheet_for_unknown_exchange_raises():
    with pytest.raises(ValueError, match="no balance-sheet mapping"):
        sheet_for("JP.7203")


def test_roe_prefers_hk_label():
    assert roe_of({"净资产收益率(ROE)": 25.0}) == pytest.approx(0.25)


def test_roe_accepts_us_fullwidth_label():
    assert roe_of({"净资产收益率（ROE）": 30.0}) == pytest.approx(0.30)


def test_roe_accepts_a_share_label():
    assert roe_of({"净资产收益率_加权公布值": 18.0}) == pytest.approx(0.18)


def test_roe_returns_none_when_absent():
    assert roe_of({"净资产收益率(ROE)": None, "其他": 1.0}) is None
    assert roe_of({}) is None


def test_roe_ignores_zero_looking_absence():
    # A genuine zero is a figure; it is the caller's job to judge it.
    assert roe_of({"净资产收益率(ROE)": 0.0}) == 0.0


def test_field_names_covers_all_groups():
    names = field_names()
    assert names["roe"]
    assert set(names["cash"]) == {"HK", "US", "CN"}
    assert set(names["debt"]) == {"HK", "US", "CN"}
