import math

from app.schemas import (
    ESTIMATION_WINDOW,
    FORECAST_HORIZON,
    DirectionModelInfo,
    ForecastPoint,
    ForecastResponse,
)
from app.services.direction_features import FEATURE_COLUMNS, compute_feature_matrix
from app.services.direction_model import direction_model_state

# 68%区間・95%区間のz値。正規分布の標準的な信頼区間の目安として採用。
_Z_68 = 1.0
_Z_95 = 1.96


def _build_direction_model_info(prices: list[float]) -> tuple[DirectionModelInfo, str | None]:
    """
    現在の分類器の状態から、この銘柄向けのdirection_model情報と、
    中心線を差し替えるべき方向（'up'/'down'/None）を組み立てる。
    """
    status = direction_model_state.status()

    predicted_direction = None
    features_row = compute_feature_matrix(prices).iloc[-1]
    if not features_row.isna().any():
        predicted_direction = direction_model_state.predict_direction(features_row[FEATURE_COLUMNS].to_dict())

    info = DirectionModelInfo(
        adopted=status.adopted,
        predicted_direction=predicted_direction,
        hit_rate=status.hit_rate,
        baseline_hit_rate=status.baseline_hit_rate,
        p_value=status.p_value,
        independent_sample_count=status.independent_sample_count,
        trained_at=status.trained_at,
    )
    override_direction = predicted_direction if status.adopted else None
    return info, override_direction


def calculate_forecast(
    stock_code: str, current_price: float, prices: list[float]
) -> ForecastResponse:
    """
    直近 ESTIMATION_WINDOW 営業日の対数リターンから、ドリフト付きランダムウォークで
    FORECAST_HORIZON 営業日先までの中心線と68%/95%レンジ帯を計算する。
    方向分類器が採用済み(adopted)の場合、中心線の符号のみをその予測方向に合わせる
    （大きさ=|mu|は変更しない）。

    prices: 古い順（index 0 が最古）の終値リスト。最低 ESTIMATION_WINDOW + 1 件。
    """
    window = prices[-(ESTIMATION_WINDOW + 1) :]
    log_returns = [math.log(window[i] / window[i - 1]) for i in range(1, len(window))]

    mu = sum(log_returns) / len(log_returns)
    variance = sum((r - mu) ** 2 for r in log_returns) / len(log_returns)
    sigma = math.sqrt(variance)

    direction_info, override_direction = _build_direction_model_info(prices)
    mu_effective = mu
    if override_direction == "up" and mu <= 0:
        mu_effective = -mu
    elif override_direction == "down" and mu >= 0:
        mu_effective = -mu

    points: list[ForecastPoint] = []
    for h in range(1, FORECAST_HORIZON + 1):
        drift = mu_effective * h
        band = sigma * math.sqrt(h)
        points.append(
            ForecastPoint(
                step=h,
                center=current_price * math.exp(drift),
                upper_68=current_price * math.exp(drift + _Z_68 * band),
                lower_68=current_price * math.exp(drift - _Z_68 * band),
                upper_95=current_price * math.exp(drift + _Z_95 * band),
                lower_95=current_price * math.exp(drift - _Z_95 * band),
            )
        )

    return ForecastResponse(
        stock_code=stock_code, horizon=FORECAST_HORIZON, points=points, direction_model=direction_info
    )
