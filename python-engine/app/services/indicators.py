import math

import pandas as pd
import pandas_ta as ta

from app.schemas import BollingerValue, Indicators, MACDValue


def _to_float_or_none(value) -> float | None:
    """pandas/numpy の値を float に変換。NaN・None・変換不可は None を返す。"""
    if value is None:
        return None
    try:
        f = float(value)
        return None if math.isnan(f) else f
    except (TypeError, ValueError):
        return None


def calculate_indicators(prices: list[float]) -> Indicators:
    """
    prices: 古い順（index 0 が最古）の価格リスト。最低 14 件。
    RSI(14)、MACD(12/26/9)、BB(20/2) を計算して Indicators を返す。
    計算不可の値は None。
    """
    close = pd.Series(prices, dtype=float)

    # --- RSI(14) ---
    rsi_series = ta.rsi(close, length=14)
    rsi_val = _to_float_or_none(rsi_series.iloc[-1] if rsi_series is not None and len(rsi_series) > 0 else None)

    # --- MACD(12, 26, 9) ---
    # pandas_ta.macd requires slow+signal-1=34 data points; use manual EWM to
    # support as few as 26 (slow period) data points for the MACD line.
    if len(prices) >= 26:
        ema_fast = close.ewm(span=12, adjust=False).mean()
        ema_slow = close.ewm(span=26, adjust=False).mean()
        macd_line_series = ema_fast - ema_slow
        signal_series = macd_line_series.ewm(span=9, adjust=False).mean()
        hist_series = macd_line_series - signal_series
        macd_line = _to_float_or_none(macd_line_series.iloc[-1])
        macd_signal = _to_float_or_none(signal_series.iloc[-1])
        macd_hist = _to_float_or_none(hist_series.iloc[-1])
    else:
        macd_line = macd_signal = macd_hist = None

    # --- Bollinger Bands(20, 2σ) ---
    # pandas_ta 0.4.71b0 with pandas 3.x produces columns named BBU_20_2.0_2.0
    # (std appears twice in the suffix). Detect actual column names at runtime.
    bb_df = ta.bbands(close, length=20, std=2)
    if bb_df is not None and not bb_df.empty:
        cols = list(bb_df.columns)
        # Find upper/middle/lower columns by prefix matching
        col_upper = next((c for c in cols if c.startswith("BBU_")), None)
        col_middle = next((c for c in cols if c.startswith("BBM_")), None)
        col_lower = next((c for c in cols if c.startswith("BBL_")), None)
        bb_upper = _to_float_or_none(bb_df[col_upper].iloc[-1]) if col_upper else None
        bb_middle = _to_float_or_none(bb_df[col_middle].iloc[-1]) if col_middle else None
        bb_lower = _to_float_or_none(bb_df[col_lower].iloc[-1]) if col_lower else None
    else:
        bb_upper = bb_middle = bb_lower = None

    return Indicators(
        rsi=rsi_val,
        macd=MACDValue(line=macd_line, signal=macd_signal, histogram=macd_hist),
        bollinger=BollingerValue(upper=bb_upper, middle=bb_middle, lower=bb_lower),
    )
