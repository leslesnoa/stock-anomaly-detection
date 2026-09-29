from dataclasses import dataclass

import pandas as pd

from app.schemas import ESTIMATION_WINDOW
from app.services.direction_features import (
    FEATURE_COLUMNS,
    compute_baseline_direction,
    compute_feature_matrix,
    generate_direction_labels,
)


@dataclass
class StockPriceSeries:
    stock_code: str
    dates: list[str]  # 古い順、pricesと同じ長さ
    prices: list[float]


def build_pooled_dataset(stocks: list[StockPriceSeries], horizon: int) -> pd.DataFrame:
    """
    各銘柄について特徴量・ベースライン方向・ラベルを計算し、日付をまたいで1つの
    DataFrameにプールする。ウォームアップ不足・ラベル未確定の行は自動的に除外される。
    """
    columns = ["date", "stock_code", *FEATURE_COLUMNS, "baseline_direction", "label"]
    frames = []
    for series in stocks:
        if len(series.prices) != len(series.dates):
            raise ValueError(f"{series.stock_code}: dates and prices length mismatch")

        df = compute_feature_matrix(series.prices)
        df["date"] = series.dates
        df["stock_code"] = series.stock_code
        df["baseline_direction"] = compute_baseline_direction(series.prices, ESTIMATION_WINDOW).values
        df["label"] = generate_direction_labels(series.prices, horizon).values
        frames.append(df.dropna()[columns])

    if not frames:
        return pd.DataFrame(columns=columns)

    return pd.concat(frames, ignore_index=True).sort_values("date").reset_index(drop=True)
