from app.services.adoption_state import AdoptionState, update_adoption_state


def test_adopts_after_consecutive_successes():
    state = AdoptionState()
    for _ in range(4):
        state = update_adoption_state(state, True, window=5)
        assert state.adopted is False
    state = update_adoption_state(state, True, window=5)
    assert state.adopted is True


def test_does_not_adopt_if_streak_broken():
    state = AdoptionState()
    for _ in range(4):
        state = update_adoption_state(state, True, window=5)
    state = update_adoption_state(state, False, window=5)
    assert state.adopted is False
    # 直後にまた4回成功しても、直近5回の窓に失敗が1回含まれる限り採用されない
    for _ in range(4):
        state = update_adoption_state(state, True, window=5)
    assert state.adopted is False


def test_stays_adopted_unless_majority_fail():
    state = AdoptionState(recent_results=(True, True, True, True, True), adopted=True)
    # 直近5回中2回失敗 -> 過半数ではないので採用維持
    state = update_adoption_state(state, False, window=5)
    state = update_adoption_state(state, False, window=5)
    assert state.adopted is True


def test_reverts_when_majority_fail():
    state = AdoptionState(recent_results=(True, True, True, True, True), adopted=True)
    # 直近5回中3回失敗 -> 過半数なので不採用に戻る
    for _ in range(3):
        state = update_adoption_state(state, False, window=5)
    assert state.adopted is False
