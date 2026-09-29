import datetime

import numpy as np
import pytest

from app.services.direction_dataset import StockPriceSeries
from app.services.direction_model import evaluate_direction_model


def _dates(n: int) -> list[str]:
    start = datetime.date(2020, 1, 1)
    return [(start + datetime.timedelta(days=i)).isoformat() for i in range(n)]


def _gbm_prices(rng: np.random.Generator, n_days: int, start_price: float = 1000.0) -> list[float]:
    """方向に関する情報を一切持たない、ドリフトゼロの幾何ブラウン運動系列を生成する。"""
    daily_returns = rng.normal(loc=0.0, scale=0.02, size=n_days)
    return (start_price * np.exp(np.cumsum(daily_returns))).tolist()


@pytest.mark.slow
def test_negative_control_false_positive_rate_matches_significance_level():
    """
    方向を持たない合成データに対して、採用基準（meets_criteria）が有意水準αに
    近い頻度でしか真にならないことを確認する。この検定が壊れていると
    （p値が過小評価されるバグがあると）ここで異常に高い採用率として現れる。
    """
    # n_days=900・n_stocks=4は、evaluate_direction_modelのMIN_INDEPENDENT_SAMPLES（60）
    # ガードを確実に上回らせるための最小限のサイズ。この値が小さすぎる
    # （例: 300営業日）と、walk-forwardのfold分割・非重複サンプリング後の独立サンプル数が
    # 60を割り込み、対比較検定に一度も到達しないまま毎回meets_criteria=Falseで
    # 早期リターンしてしまう。その場合このテストは「検定ロジックを一度も実行せずに
    # 誤って合格する」安全網として機能しない偽陰性になるため、必ずガードを超えるサイズにする。
    n_trials = 50
    n_stocks = 4
    n_days = 900

    false_positives = 0
    independent_sample_counts = []
    for seed in range(n_trials):
        rng = np.random.default_rng(seed)
        dates = _dates(n_days)
        stocks = [
            StockPriceSeries(stock_code=f"TEST{i}", dates=dates, prices=_gbm_prices(rng, n_days))
            for i in range(n_stocks)
        ]
        result = evaluate_direction_model(stocks)
        independent_sample_counts.append(result.independent_sample_count)
        if result.meets_criteria:
            false_positives += 1

    # 検定ロジックに実際に到達したことを確認する（到達していなければ上の
    # false_positive_rateの合格は無意味なので、このテスト自体の前提を保証する）。
    assert min(independent_sample_counts) >= 60

    false_positive_rate = false_positives / n_trials
    # 有意水準0.05に対し、二項分布のばらつきを考慮した緩い上限（0.20）で判定する。
    # p値過小評価バグがあれば採用率は50%超など明らかに逸脱した値になるため、
    # この緩い上限でも検出力は十分。
    assert false_positive_rate <= 0.20, (
        f"採用率が有意水準から乖離: {false_positive_rate:.2%}（p値が過小評価されている可能性）"
    )
