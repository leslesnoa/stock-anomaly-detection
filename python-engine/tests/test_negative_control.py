import datetime

import numpy as np
import pytest

from app.services.direction_dataset import StockPriceSeries
from app.services.direction_model import MIN_INDEPENDENT_SAMPLES, evaluate_direction_model


def _dates(n: int) -> list[str]:
    start = datetime.date(2020, 1, 1)
    return [(start + datetime.timedelta(days=i)).isoformat() for i in range(n)]


def _prices_from_returns(daily_returns: np.ndarray, start_price: float = 1000.0) -> list[float]:
    return (start_price * np.exp(np.cumsum(daily_returns))).tolist()


def _gbm_prices(rng: np.random.Generator, n_days: int, start_price: float = 1000.0) -> list[float]:
    """方向に関する情報を一切持たない、ドリフトゼロの幾何ブラウン運動系列を生成する。"""
    return _prices_from_returns(rng.normal(loc=0.0, scale=0.02, size=n_days), start_price)


def _correlated_gbm_prices(
    rng: np.random.Generator, n_days: int, n_stocks: int, rho: float
) -> list[list[float]]:
    """
    共通の市場要因を持つドリフトゼロ系列をn_stocks本生成する。
    r_i = sqrt(rho) * market + sqrt(1 - rho) * idio_i なので、銘柄間の日次リターン相関は
    ちょうどrhoになる（marketとidio_iは独立同分布）。ドリフトは0のままなので
    「方向に関する情報がない」というネガティブコントロールの前提は崩れない。
    """
    market = rng.normal(loc=0.0, scale=0.02, size=n_days)
    series = []
    for _ in range(n_stocks):
        idio = rng.normal(loc=0.0, scale=0.02, size=n_days)
        series.append(_prices_from_returns(np.sqrt(rho) * market + np.sqrt(1.0 - rho) * idio))
    return series


@pytest.mark.slow
def test_negative_control_false_positive_rate_matches_significance_level():
    """
    方向を持たない（かつ銘柄間で無相関な）合成データに対して、採用基準（meets_criteria）が
    有意水準αに近い頻度でしか真にならないことを確認する。これは検定そのものが
    素直なケースで較正されていることのチェックであり、銘柄間相関の扱いは
    test_negative_control_correlated_stocks_false_positive_rate が担当する。
    """
    # n_days=1200は、日付単位集約後のindependent_sample_count（実測44件）を
    # MIN_INDEPENDENT_SAMPLES（30）に対して余裕を持って上回らせるためのサイズ。
    # 小さすぎると対比較検定に一度も到達しないまま毎回meets_criteria=Falseで
    # 早期リターンし、このテストが「検定ロジックを一度も実行せずに誤って合格する」
    # 偽陰性の安全網になってしまう。下のassertでそれを明示的に防いでいる。
    n_trials = 50
    n_stocks = 4
    n_days = 1200

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

    # 検定ロジックに実際に到達したことを確認する（到達していなければ下の
    # false_positive_rateの合格は無意味なので、このテスト自体の前提を保証する）。
    assert min(independent_sample_counts) >= MIN_INDEPENDENT_SAMPLES

    false_positive_rate = false_positives / n_trials
    # 有意水準0.05に対し、50試行の二項分布のばらつきを考慮した上限（0.20）で判定する。
    # 無相関ケースではp値過小評価バグは50%超など明らかに逸脱した値として現れるため、
    # この緩い上限でも検出力は十分。相関ありケースの厳しい判定は下のテストで行う。
    assert false_positive_rate <= 0.20, (
        f"採用率が有意水準から乖離: {false_positive_rate:.2%}（p値が過小評価されている可能性）"
    )


@pytest.mark.slow
def test_negative_control_correlated_stocks_false_positive_rate():
    """
    同一日付の複数銘柄が市場要因で相関している場合でも、採用率が有意水準付近に
    留まることを確認する回帰テスト。

    日付単位集約（aggregate_daily_outcomes）を入れる前は、同じ日の銘柄ごとの行を
    独立試行として数えていたため、相関のあるデータでは採用率が大きく膨らんでいた。
    実測（rho=0.6・20銘柄・1200営業日・200試行）では集約前が18.0%、集約後が3.0%。
    上の無相関テストはこの失敗モードを構造的に再現できないため、こちらが本命の安全網。
    """
    # rho=0.6は日本株の日次リターン相関としてやや高めだが現実的な中位値。
    # 20銘柄はwatchlistとして十分あり得る規模で、かつ「銘柄数分だけ独立試行を
    # 水増しする」というバグの効き方を強く出せるため、回帰テストとしての検出力が高い。
    # n_days=1200でindependent_sample_countは実測44件（MIN_INDEPENDENT_SAMPLES=30超）。
    n_trials = 200
    n_stocks = 20
    n_days = 1200
    rho = 0.6

    false_positives = 0
    independent_sample_counts = []
    dates = _dates(n_days)
    for seed in range(n_trials):
        rng = np.random.default_rng(seed)
        series = _correlated_gbm_prices(rng, n_days, n_stocks, rho)
        stocks = [
            StockPriceSeries(stock_code=f"TEST{i}", dates=dates, prices=prices)
            for i, prices in enumerate(series)
        ]
        result = evaluate_direction_model(stocks)
        independent_sample_counts.append(result.independent_sample_count)
        if result.meets_criteria:
            false_positives += 1

    # 検定に到達していることの自己検証（早期リターンで素通りしていないこと）。
    assert min(independent_sample_counts) >= MIN_INDEPENDENT_SAMPLES

    false_positive_rate = false_positives / n_trials
    # 上限0.08の根拠: 真の採用率が有意水準0.05なら200試行の期待値は10件、
    # 0.08は16件に相当し、二項分布のもとで超える確率は約2%（実測は6件=3.0%）。
    # 一方、集約前の実装では36件=18.0%で明確に超える。両側に十分な余裕がある。
    # n_trialsを既存テストの50ではなく200にしているのは、50試行だと二項ノイズが
    # 大きすぎて「バグありなら必ず落ちる」性質を担保できないため（実測で確認済み）。
    assert false_positive_rate <= 0.08, (
        f"相関のある銘柄で採用率が有意水準から乖離: {false_positive_rate:.2%}"
        "（同一日付の複数銘柄を独立試行として数えていないか確認すること）"
    )
