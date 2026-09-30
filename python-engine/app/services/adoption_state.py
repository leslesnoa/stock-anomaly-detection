from dataclasses import dataclass


@dataclass(frozen=True)
class AdoptionState:
    """直近window回の再学習サイクルでの採用基準充足履歴と、現在の採用可否。"""

    recent_results: tuple[bool, ...] = ()
    adopted: bool = False


def update_adoption_state(state: AdoptionState, meets_criteria: bool, window: int) -> AdoptionState:
    """
    24時間ごとの再検定をそのままadopted判定に使うと多重検定でα膨張するため、
    直近window回のサイクルの結果でヒステリシスをかける。
    - 未採用→採用: 直近window回**連続**で基準を満たした場合のみ
    - 採用→未採用: 直近window回のうち**過半数**が基準を満たさなくなった場合のみ
    - それ以外: 現状維持
    """
    history = (state.recent_results + (meets_criteria,))[-window:]
    if state.adopted:
        failed = sum(1 for r in history if not r)
        new_adopted = failed <= len(history) // 2
    else:
        new_adopted = len(history) == window and all(history)
    return AdoptionState(recent_results=history, adopted=new_adopted)
