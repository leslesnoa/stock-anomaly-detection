import numpy as np
import pandas as pd
import pandas_ta as ta

_MOMENTUM_WINDOWS = (5, 20, 60)
_RSI_LENGTH = 14
_MACD_FAST, _MACD_SLOW, _MACD_SIGNAL = 12, 26, 9
_VOLATILITY_WINDOW = 20

FEATURE_COLUMNS = [
    "momentum_5",
    "momentum_20",
    "momentum_60",
    "rsi_14",
    "macd_hist",
    "volatility_20",
]


def compute_feature_matrix(prices: list[float]) -> pd.DataFrame:
    """
    prices: 古い順の終値リスト。
    行iはprices[i]時点の特徴量。ウォームアップ不足の行はNaN（呼び出し側でdropnaする）。
    """
    close = pd.Series(prices, dtype=float)

    features = pd.DataFrame(index=close.index)
    for window in _MOMENTUM_WINDOWS:
        features[f"momentum_{window}"] = close / close.shift(window) - 1.0

    features["rsi_14"] = ta.rsi(close, length=_RSI_LENGTH)

    ema_fast = close.ewm(span=_MACD_FAST, adjust=False).mean()
    ema_slow = close.ewm(span=_MACD_SLOW, adjust=False).mean()
    macd_line = ema_fast - ema_slow
    macd_signal = macd_line.ewm(span=_MACD_SIGNAL, adjust=False).mean()
    features["macd_hist"] = macd_line - macd_signal

    log_return = np.log(close / close.shift(1))
    features["volatility_20"] = log_return.rolling(_VOLATILITY_WINDOW).std()

    return features[FEATURE_COLUMNS]


def compute_baseline_direction(prices: list[float], window: int) -> pd.Series:
    """
    既存のforecast.calculate_forecastと同じ定義（直近window営業日の対数リターン平均、
    その符号）で「時点tにおいて中心線が示す方向」を返す。1.0=強気(up)、0.0=弱気(down)。
    windowに満たない先頭はNaN。
    """
    close = pd.Series(prices, dtype=float)
    log_return = np.log(close / close.shift(1))
    mu = log_return.rolling(window).mean()
    direction = (mu > 0).astype(float)
    direction[mu.isna()] = float("nan")
    return direction


def generate_direction_labels(prices: list[float], horizon: int) -> pd.Series:
    """
    時点tのラベル = prices[t+horizon] > prices[t] なら1.0、そうでなければ0.0。
    直近horizon件はラベル未確定のためNaN。
    """
    close = pd.Series(prices, dtype=float)
    future = close.shift(-horizon)
    label = (future > close).astype(float)
    label[future.isna()] = float("nan")
    return label
