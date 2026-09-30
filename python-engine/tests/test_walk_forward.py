import numpy as np
import pytest

from app.services.walk_forward import (
    aggregate_daily_outcomes,
    make_walk_forward_folds,
    non_overlapping_sample,
    paired_significance_test,
)


def _dates(n: int) -> list[str]:
    import datetime

    start = datetime.date(2024, 1, 1)
    return [(start + datetime.timedelta(days=i)).isoformat() for i in range(n)]


def test_make_walk_forward_folds_respects_embargo():
    dates = _dates(300)
    folds = make_walk_forward_folds(dates, n_folds=5, embargo=20)
    assert len(folds) > 0
    for fold in folds:
        train_end_date = fold.train_dates[-1]
        test_start_date = fold.test_dates[0]
        train_end_idx = dates.index(train_end_date)
        test_start_idx = dates.index(test_start_date)
        assert test_start_idx - train_end_idx >= 20


def test_make_walk_forward_folds_excludes_folds_with_too_little_training_data():
    dates = _dates(300)
    folds = make_walk_forward_folds(dates, n_folds=5, embargo=20)
    for fold in folds:
        assert len(fold.train_dates) >= 20 * 3


def test_make_walk_forward_folds_empty_when_too_few_dates():
    dates = _dates(10)
    folds = make_walk_forward_folds(dates, n_folds=5, embargo=20)
    assert folds == []


def test_non_overlapping_sample_steps_by_horizon():
    dates = _dates(100)
    sampled = non_overlapping_sample(dates, step=20)
    assert sampled == dates[::20]
    assert len(sampled) == 5


def test_aggregate_daily_outcomes_model_wins_a_date():
    # 1日分・3銘柄: モデル2的中 vs ベースライン1的中 → モデルの勝ち
    dates = np.array(["2024-01-01"] * 3)
    model = np.array([True, True, False])
    baseline = np.array([True, False, False])
    model_wins, baseline_wins = aggregate_daily_outcomes(dates, model, baseline)
    assert model_wins.tolist() == [True]
    assert baseline_wins.tolist() == [False]


def test_aggregate_daily_outcomes_baseline_wins_a_date():
    # 1日分・3銘柄: モデル0的中 vs ベースライン2的中 → ベースラインの勝ち
    dates = np.array(["2024-01-01"] * 3)
    model = np.array([False, False, False])
    baseline = np.array([True, True, False])
    model_wins, baseline_wins = aggregate_daily_outcomes(dates, model, baseline)
    assert model_wins.tolist() == [False]
    assert baseline_wins.tolist() == [True]


def test_aggregate_daily_outcomes_tie_date_counts_for_neither():
    # 1日分・4銘柄: 的中数が2対2の引き分け → どちらの勝ちでもない（McNemarの一致ペア相当）
    dates = np.array(["2024-01-01"] * 4)
    model = np.array([True, True, False, False])
    baseline = np.array([False, False, True, True])
    model_wins, baseline_wins = aggregate_daily_outcomes(dates, model, baseline)
    assert model_wins.tolist() == [False]
    assert baseline_wins.tolist() == [False]


def test_aggregate_daily_outcomes_multiple_dates_are_independent():
    # 3日×2銘柄。日付は昇順にグルーピングされる。
    #   2024-01-01: モデル2 vs ベース0 → モデルの勝ち
    #   2024-01-02: モデル0 vs ベース2 → ベースラインの勝ち
    #   2024-01-03: モデル1 vs ベース1 → 引き分け
    dates = np.array(
        ["2024-01-01", "2024-01-01", "2024-01-02", "2024-01-02", "2024-01-03", "2024-01-03"]
    )
    model = np.array([True, True, False, False, True, False])
    baseline = np.array([False, False, True, True, False, True])
    model_wins, baseline_wins = aggregate_daily_outcomes(dates, model, baseline)
    assert model_wins.tolist() == [True, False, False]
    assert baseline_wins.tolist() == [False, True, False]
    # 行数はstock-dayの6件ではなく日付の3件に縮む（独立サンプル数の定義そのもの）
    assert len(model_wins) == 3


def test_aggregate_daily_outcomes_output_feeds_paired_significance_test():
    # 10日すべてでモデルが勝つ → 片側厳密二項検定で p = 0.5**10 ≈ 0.00098
    dates = np.array([f"2024-01-{i + 1:02d}" for i in range(10)])
    model = np.array([True] * 10)
    baseline = np.array([False] * 10)
    model_wins, baseline_wins = aggregate_daily_outcomes(dates, model, baseline)
    p = paired_significance_test(model_wins, baseline_wins)
    assert p == pytest.approx(0.5**10)


def test_paired_significance_test_significant_when_model_much_better():
    rng = np.random.default_rng(0)
    n = 200
    baseline_correct = rng.random(n) < 0.5
    # モデルは常にbaselineが外した分も含めて的中する、という極端なケース
    model_correct = np.ones(n, dtype=bool)
    p = paired_significance_test(model_correct, baseline_correct)
    assert p < 0.05


def test_paired_significance_test_not_significant_when_equal():
    rng = np.random.default_rng(1)
    n = 200
    model_correct = rng.random(n) < 0.5
    baseline_correct = model_correct.copy()
    p = paired_significance_test(model_correct, baseline_correct)
    assert p == 1.0  # 完全に一致 = 食い違いゼロ


def test_paired_significance_test_no_disagreement_returns_one():
    model_correct = np.array([True, True, False, False])
    baseline_correct = np.array([True, True, False, False])
    p = paired_significance_test(model_correct, baseline_correct)
    assert p == 1.0
