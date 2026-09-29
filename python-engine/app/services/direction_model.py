import threading
from dataclasses import dataclass, field
from datetime import datetime, timezone

import numpy as np
from sklearn.linear_model import LogisticRegression
from sklearn.preprocessing import StandardScaler

from app.schemas import FORECAST_HORIZON
from app.services.direction_dataset import StockPriceSeries, build_pooled_dataset
from app.services.direction_features import FEATURE_COLUMNS
from app.services.adoption_state import AdoptionState, update_adoption_state
from app.services.walk_forward import (
    make_walk_forward_folds,
    non_overlapping_sample,
    paired_significance_test,
)

MIN_TRAINING_ROWS = 200
MIN_INDEPENDENT_SAMPLES = 60
N_FOLDS = 5
HYSTERESIS_WINDOW = 5
SIGNIFICANCE_LEVEL = 0.05
MIN_FOLD_TRAIN_ROWS = 30

_Pipeline = tuple[StandardScaler, LogisticRegression]


@dataclass
class EvaluationResult:
    hit_rate: float | None
    baseline_hit_rate: float | None
    p_value: float | None
    independent_sample_count: int
    meets_criteria: bool
    pipeline: _Pipeline | None


def _fit_pipeline(frame, feature_columns: list[str]) -> _Pipeline:
    scaler = StandardScaler().fit(frame[feature_columns])
    clf = LogisticRegression(max_iter=1000).fit(scaler.transform(frame[feature_columns]), frame["label"])
    return scaler, clf


def evaluate_direction_model(
    stocks: list[StockPriceSeries], horizon: int = FORECAST_HORIZON
) -> EvaluationResult:
    """
    walk-forwardで既存ドリフト中心線に対する優位性を検証する純粋関数。
    ヒステリシス（連続再学習にまたがる採用判定）は呼び出し側（DirectionModelState）が行う。
    """
    dataset = build_pooled_dataset(stocks, horizon)
    if len(dataset) < MIN_TRAINING_ROWS:
        return EvaluationResult(None, None, None, 0, False, None)

    sorted_dates = sorted(dataset["date"].unique())
    folds = make_walk_forward_folds(sorted_dates, n_folds=N_FOLDS, embargo=horizon)

    model_correct_all: list[bool] = []
    baseline_correct_all: list[bool] = []

    for fold in folds:
        train_df = dataset[dataset["date"].isin(fold.train_dates)]
        test_df = dataset[dataset["date"].isin(fold.test_dates)]
        if len(train_df) < MIN_FOLD_TRAIN_ROWS or len(test_df) == 0:
            continue

        sampled_dates = set(non_overlapping_sample(sorted(test_df["date"].unique()), step=horizon))
        test_df = test_df[test_df["date"].isin(sampled_dates)]
        if len(test_df) == 0:
            continue

        scaler, clf = _fit_pipeline(train_df, FEATURE_COLUMNS)
        predictions = clf.predict(scaler.transform(test_df[FEATURE_COLUMNS]))

        model_correct_all.extend((predictions == test_df["label"]).tolist())
        baseline_correct_all.extend((test_df["baseline_direction"] == test_df["label"]).tolist())

    independent_sample_count = len(model_correct_all)
    if independent_sample_count < MIN_INDEPENDENT_SAMPLES:
        return EvaluationResult(None, None, None, independent_sample_count, False, None)

    model_correct = np.array(model_correct_all)
    baseline_correct = np.array(baseline_correct_all)
    hit_rate = float(model_correct.mean())
    baseline_hit_rate = float(baseline_correct.mean())
    p_value = paired_significance_test(model_correct, baseline_correct)

    meets_criteria = hit_rate > baseline_hit_rate and p_value < SIGNIFICANCE_LEVEL
    pipeline = _fit_pipeline(dataset, FEATURE_COLUMNS)

    return EvaluationResult(hit_rate, baseline_hit_rate, p_value, independent_sample_count, meets_criteria, pipeline)


@dataclass
class DirectionModelStatus:
    adopted: bool = False
    hit_rate: float | None = None
    baseline_hit_rate: float | None = None
    p_value: float | None = None
    independent_sample_count: int | None = None
    trained_at: str | None = None
    recent_results: tuple[bool, ...] = ()


class DirectionModelState:
    """python-engineプロセス内でモデルとその検証結果を保持するインメモリの状態。
    /model/train（書き込み）と /forecast（読み込み）が別スレッドから同時にアクセスしうるため
    threading.Lock で排他制御する。"""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._pipeline: _Pipeline | None = None
        self._status = DirectionModelStatus()
        self._adoption = AdoptionState()
        self._training = False

    def status(self) -> DirectionModelStatus:
        with self._lock:
            return self._status

    def try_begin_training(self) -> bool:
        """既に学習中ならFalseを返して即座に諦める（/model/train の多重起動対策）。"""
        with self._lock:
            if self._training:
                return False
            self._training = True
            return True

    def train(self, stocks: list[StockPriceSeries]) -> DirectionModelStatus:
        try:
            result = evaluate_direction_model(stocks)
            with self._lock:
                self._adoption = update_adoption_state(self._adoption, result.meets_criteria, HYSTERESIS_WINDOW)
                if result.pipeline is not None:
                    self._pipeline = result.pipeline
                self._status = DirectionModelStatus(
                    adopted=self._adoption.adopted,
                    hit_rate=result.hit_rate,
                    baseline_hit_rate=result.baseline_hit_rate,
                    p_value=result.p_value,
                    independent_sample_count=result.independent_sample_count,
                    trained_at=datetime.now(timezone.utc).isoformat(),
                    recent_results=self._adoption.recent_results,
                )
                return self._status
        finally:
            with self._lock:
                self._training = False

    def predict_direction(self, features: dict[str, float]) -> str | None:
        """学習済みモデルがあれば方向を返す（adoptedかどうかは呼び出し側が判断する）。
        未学習ならNone。"""
        with self._lock:
            pipeline = self._pipeline
        if pipeline is None:
            return None
        scaler, clf = pipeline
        row = [[features[col] for col in FEATURE_COLUMNS]]
        prediction = clf.predict(scaler.transform(row))[0]
        return "up" if prediction == 1 else "down"

    def reset(self) -> None:
        """テスト用: 状態を初期化する。"""
        with self._lock:
            self._pipeline = None
            self._status = DirectionModelStatus()
            self._adoption = AdoptionState()
            self._training = False


direction_model_state = DirectionModelState()
