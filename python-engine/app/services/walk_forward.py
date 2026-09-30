from dataclasses import dataclass

import numpy as np
import pandas as pd
from scipy import stats


@dataclass(frozen=True)
class WalkForwardFold:
    train_dates: list[str]
    test_dates: list[str]


def make_walk_forward_folds(
    sorted_unique_dates: list[str], n_folds: int, embargo: int
) -> list[WalkForwardFold]:
    """
    sorted_unique_dates: プール後の一意な日付の昇順リスト。
    拡大窓walk-forward: フォールドiの学習データは「フォールドiより前の全期間」、
    検証データは「フォールドi自身の期間」。学習末尾と検証開始の間に embargo 日分の
    空白を設け、ホライズン分のラベルが検証期間の価格を参照するリークを防ぐ。
    学習期間がembargoの3倍未満のフォールドは統計的に無意味なため除外する。
    """
    n = len(sorted_unique_dates)
    if n_folds < 2 or n == 0:
        return []
    fold_size = max(n // n_folds, 1)

    folds: list[WalkForwardFold] = []
    for i in range(1, n_folds):
        test_start = i * fold_size
        if test_start >= n:
            break
        test_end = n if i == n_folds - 1 else min((i + 1) * fold_size, n)
        train_end = test_start - embargo
        if train_end < embargo * 3:
            continue
        folds.append(
            WalkForwardFold(
                train_dates=sorted_unique_dates[:train_end],
                test_dates=sorted_unique_dates[test_start:test_end],
            )
        )
    return folds


def non_overlapping_sample(sorted_dates: list[str], step: int) -> list[str]:
    """
    ホライズン分（step）の間隔を空けた非重複な日付だけを抽出する。
    隣接ラベルが同じ将来価格を参照して自己相関することによる検定のp値過小評価を防ぐ。
    """
    return sorted_dates[::step]


def aggregate_daily_outcomes(
    dates: np.ndarray, model_correct: np.ndarray, baseline_correct: np.ndarray
) -> tuple[np.ndarray, np.ndarray]:
    """
    同一日付でプールした複数銘柄は市場要因で相関するため、そのまま独立試行として
    数えると有意性検定が過大評価される（設計仕様書 2026-09-29-chart-direction-classifier-design.md
    113-115行参照）。日付ごとにモデル的中数とベースライン的中数を比較し、多い方を
    「その日の勝者」とする一対比較に還元する（同数はどちらの勝者でもない＝McNemarの
    一致ペアと同様に除外）。戻り値は日付ごとの「モデルが勝った日」「ベースラインが
    勝った日」を示す等長のbool配列で、paired_significance_test にそのまま渡せる
    （model_correct/baseline_correctと同じ意味を持つが、行はstock-dayではなく日付）。
    """
    df = pd.DataFrame(
        {"date": dates, "model_correct": model_correct, "baseline_correct": baseline_correct}
    )
    per_date = df.groupby("date").agg(
        model_hits=("model_correct", "sum"),
        baseline_hits=("baseline_correct", "sum"),
    )
    model_wins = (per_date["model_hits"] > per_date["baseline_hits"]).to_numpy()
    baseline_wins = (per_date["baseline_hits"] > per_date["model_hits"]).to_numpy()
    return model_wins, baseline_wins


def paired_significance_test(model_correct: np.ndarray, baseline_correct: np.ndarray) -> float:
    """
    対比較検定（厳密二項検定によるMcNemar検定）。
    b = ベースラインだけ的中した件数、c = モデルだけ的中した件数。
    帰無仮説「bとcは同確率」に対する片側p値（モデルが優れている方向）を返す。
    食い違いが一件もない場合は優位性なしとしてp値1.0を返す。
    """
    b = int(np.sum(baseline_correct & ~model_correct))
    c = int(np.sum(model_correct & ~baseline_correct))
    if b + c == 0:
        return 1.0
    return stats.binomtest(c, b + c, p=0.5, alternative="greater").pvalue
