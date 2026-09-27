import math

from app.schemas import ESTIMATION_WINDOW, FORECAST_HORIZON, ForecastPoint, ForecastResponse

# 68%区間・95%区間のz値。正規分布の標準的な信頼区間の目安として採用。
_Z_68 = 1.0
_Z_95 = 1.96


def calculate_forecast(
    stock_code: str, current_price: float, prices: list[float]
) -> ForecastResponse:
    """
    直近 ESTIMATION_WINDOW 営業日の対数リターンから、ドリフト付きランダムウォークで
    FORECAST_HORIZON 営業日先までの中心線と68%/95%レンジ帯を計算する。

    prices: 古い順（index 0 が最古）の終値リスト。最低 ESTIMATION_WINDOW + 1 件。
    """
    window = prices[-(ESTIMATION_WINDOW + 1) :]
    log_returns = [math.log(window[i] / window[i - 1]) for i in range(1, len(window))]

    mu = sum(log_returns) / len(log_returns)
    variance = sum((r - mu) ** 2 for r in log_returns) / len(log_returns)
    sigma = math.sqrt(variance)

    points: list[ForecastPoint] = []
    for h in range(1, FORECAST_HORIZON + 1):
        drift = mu * h
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

    return ForecastResponse(stock_code=stock_code, horizon=FORECAST_HORIZON, points=points)
