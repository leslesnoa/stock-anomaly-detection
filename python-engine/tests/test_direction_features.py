import math

import numpy as np
import pandas as pd
import pytest

from app.services.direction_features import (
    FEATURE_COLUMNS,
    compute_baseline_direction,
    compute_feature_matrix,
    generate_direction_labels,
)


def _rising_prices(n: int, daily_return: float = 0.01, start: float = 1000.0) -> list[float]:
    return [start * (1 + daily_return) ** i for i in range(n)]


def test_compute_feature_matrix_has_expected_columns():
    prices = _rising_prices(150)
    features = compute_feature_matrix(prices)
    assert list(features.columns) == FEATURE_COLUMNS
    assert len(features) == len(prices)


def test_compute_feature_matrix_warmup_rows_are_nan():
    prices = _rising_prices(150)
    features = compute_feature_matrix(prices)
    # momentum_60 は60本の過去終値を要するため、先頭60行はNaN
    assert features["momentum_60"].iloc[:60].isna().all()
    assert not features["momentum_60"].iloc[65:].isna().any()


def test_compute_feature_matrix_momentum_sign_matches_trend():
    prices = _rising_prices(150, daily_return=0.01)
    features = compute_feature_matrix(prices)
    assert (features["momentum_5"].dropna() > 0).all()
    assert (features["momentum_20"].dropna() > 0).all()


def test_compute_baseline_direction_up_for_rising_prices():
    prices = _rising_prices(150, daily_return=0.01)
    baseline = compute_baseline_direction(prices, window=120)
    valid = baseline.dropna()
    assert len(valid) > 0
    assert (valid == 1.0).all()


def test_compute_baseline_direction_down_for_falling_prices():
    prices = _rising_prices(150, daily_return=-0.01)
    baseline = compute_baseline_direction(prices, window=120)
    valid = baseline.dropna()
    assert len(valid) > 0
    assert (valid == 0.0).all()


def test_compute_baseline_direction_warmup_is_nan():
    prices = _rising_prices(150)
    baseline = compute_baseline_direction(prices, window=120)
    assert baseline.iloc[:120].isna().all()


def test_generate_direction_labels_matches_manual_calculation():
    prices = [100.0, 101.0, 99.0, 105.0, 95.0, 110.0]
    labels = generate_direction_labels(prices, horizon=2)
    # t=0: prices[2]=99 < prices[0]=100 -> 0.0
    # t=1: prices[3]=105 > prices[1]=101 -> 1.0
    # t=2: prices[4]=95 < prices[2]=99 -> 0.0
    # t=3: prices[5]=110 > prices[3]=105 -> 1.0
    assert labels.iloc[0] == 0.0
    assert labels.iloc[1] == 1.0
    assert labels.iloc[2] == 0.0
    assert labels.iloc[3] == 1.0


def test_generate_direction_labels_last_horizon_rows_are_nan():
    prices = [100.0, 101.0, 99.0, 105.0, 95.0, 110.0]
    labels = generate_direction_labels(prices, horizon=2)
    assert labels.iloc[4:].isna().all()
