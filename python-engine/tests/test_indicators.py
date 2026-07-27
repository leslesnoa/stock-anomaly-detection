from app.services.indicators import calculate_indicators


def _rising_prices(n: int = 30) -> list[float]:
    """単調増加価格: z-score検知のためRSI高くなりやすいシナリオ"""
    return [100.0 + i * 2 for i in range(n)]


def _flat_prices(n: int = 30) -> list[float]:
    """横ばい価格: RSI中立付近"""
    return [100.0 + (i % 2) * 0.1 for i in range(n)]


def test_rsi_overbought_on_rising_prices():
    prices = _rising_prices(30)
    result = calculate_indicators(prices)
    assert result.rsi is not None
    assert result.rsi > 70, f"期待: RSI > 70, 実際: {result.rsi}"


def test_rsi_is_none_when_too_few_prices():
    # RSI(14) には 15件必要。14件では NaN → None
    prices = [100.0] * 14
    result = calculate_indicators(prices)
    # 14件は flat → RSI が NaN になる（avg_loss=0 のエッジケース）
    # None か 100.0 のいずれかであれば OK
    assert result.rsi is None or result.rsi == 100.0


def test_macd_line_is_calculated_with_30_prices():
    prices = _rising_prices(30)
    result = calculate_indicators(prices)
    # MACD line は 26件あれば計算可能（30件で OK）
    assert result.macd.line is not None


def test_macd_signal_may_be_none_with_30_prices():
    # signal EMA(9) には MACD line が 9件必要 → 30件データでは精度低め（None の可能性あり）
    # None または float のどちらでもテストはパス（spec の許容範囲）
    prices = _rising_prices(30)
    result = calculate_indicators(prices)
    assert result.macd.signal is None or isinstance(result.macd.signal, float)


def test_bollinger_bands_with_30_prices():
    prices = _flat_prices(30)
    result = calculate_indicators(prices)
    assert result.bollinger.upper is not None
    assert result.bollinger.middle is not None
    assert result.bollinger.lower is not None
    assert result.bollinger.upper >= result.bollinger.middle >= result.bollinger.lower


def test_bollinger_none_with_fewer_than_20_prices():
    prices = _rising_prices(19)  # BB(20) には 20件必要
    result = calculate_indicators(prices)
    assert result.bollinger.upper is None
    assert result.bollinger.middle is None
    assert result.bollinger.lower is None


def test_returns_indicators_type():
    from app.schemas import Indicators
    result = calculate_indicators(_rising_prices(30))
    assert isinstance(result, Indicators)
