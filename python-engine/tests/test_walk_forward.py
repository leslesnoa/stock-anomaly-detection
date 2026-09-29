import numpy as np

from app.services.walk_forward import (
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
