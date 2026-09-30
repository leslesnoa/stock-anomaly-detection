import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import {
  StockChart,
  sliceToDisplayWindow,
  PERIOD_TRADING_DAYS,
} from "./stock-chart";
import type {
  StockChart as StockChartData,
  StockChartPrice,
  StockChartAlertBand,
} from "@/lib/go-api-client";

// jsdomは ResizeObserver を実装しておらず、実測レイアウト（offsetWidth/offsetHeight・
// getBoundingClientRect）も常に0を返す。Rechartsの ResponsiveContainer はこれらが無いと
// 中身（Legendなど）を描画しないため、テスト用に最小限のモックと固定サイズを与える。
// 補足: インストール済みのRecharts 3.10.1のResponsiveContainerは初期サイズ計測に
// getBoundingClientRect()を使う（offsetWidth/offsetHeightではない）。両方モックしておく。
class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

let originalResizeObserver: typeof ResizeObserver | undefined;
let originalOffsetWidth: PropertyDescriptor | undefined;
let originalOffsetHeight: PropertyDescriptor | undefined;
let originalGetBoundingClientRect: typeof HTMLElement.prototype.getBoundingClientRect;

beforeAll(() => {
  originalResizeObserver = global.ResizeObserver;
  originalOffsetWidth = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    "offsetWidth",
  );
  originalOffsetHeight = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    "offsetHeight",
  );
  originalGetBoundingClientRect = HTMLElement.prototype.getBoundingClientRect;

  global.ResizeObserver = ResizeObserverMock;
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    value: 600,
  });
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    value: 400,
  });
  HTMLElement.prototype.getBoundingClientRect = () =>
    ({
      width: 600,
      height: 400,
      top: 0,
      left: 0,
      bottom: 400,
      right: 600,
      x: 0,
      y: 0,
      toJSON() {},
    }) as DOMRect;
});

afterAll(() => {
  global.ResizeObserver = originalResizeObserver as typeof ResizeObserver;
  HTMLElement.prototype.getBoundingClientRect = originalGetBoundingClientRect;
  if (originalOffsetWidth) {
    Object.defineProperty(
      HTMLElement.prototype,
      "offsetWidth",
      originalOffsetWidth,
    );
  }
  if (originalOffsetHeight) {
    Object.defineProperty(
      HTMLElement.prototype,
      "offsetHeight",
      originalOffsetHeight,
    );
  }
});

const baseData: StockChartData = {
  stock_code: "7203",
  current_price: 3250.0,
  prices: [
    { date: "2026-09-01", close: 3200.0 },
    { date: "2026-09-02", close: 3250.0 },
  ],
  alert_band: [{ date: "2026-09-02", upper: 3400.0, lower: 3000.0 }],
  current_z_score: 1.5,
  forecast: {
    horizon: 20,
    points: [
      {
        step: 1,
        center: 3260.0,
        upper_68: 3300.0,
        lower_68: 3220.0,
        upper_95: 3350.0,
        lower_95: 3180.0,
      },
    ],
    direction_model: {
      adopted: false,
      predicted_direction: null,
      hit_rate: null,
      baseline_hit_rate: null,
      p_value: null,
      independent_sample_count: null,
      trained_at: null,
    },
  },
  notifications: [
    {
      notified_at: "2026-09-02T07:00:00Z",
      anomaly_score: 3.1,
      ai_report: "急騰の背景には...",
      slack_sent: true,
    },
  ],
};

// buildPrices()（下部の sliceToDisplayWindow テスト用ヘルパー）は
// `2026-01-${(i % 28) + 1}` で28日周期の日付を使い回すため、同一サイクル内に
// 重複日付が生じる。このヘルパーは1日ずつ純増するユニークな連続暦日を返す
// （営業日/週末は考慮しない。ここでは配列インデックスの件数だけが重要）。
function buildSequentialPrices(
  n: number,
  startDate = "2026-01-01",
): StockChartPrice[] {
  const start = new Date(`${startDate}T00:00:00Z`);
  return Array.from({ length: n }, (_, i) => {
    const d = new Date(start);
    d.setUTCDate(d.getUTCDate() + i);
    return { date: d.toISOString().slice(0, 10), close: 1000 + i };
  });
}

describe("StockChart", () => {
  it("renders without the unavailable-forecast notice when forecast exists", () => {
    render(<StockChart data={baseData} />);
    expect(
      screen.queryByText("予測を取得できませんでした"),
    ).not.toBeInTheDocument();
  });

  it("shows the unavailable-forecast notice when forecast is null", () => {
    render(<StockChart data={{ ...baseData, forecast: null }} />);
    expect(screen.getByText("予測を取得できませんでした")).toBeInTheDocument();
  });

  it("labels the alert band and the statistical range separately in the legend", async () => {
    render(<StockChart data={baseData} />);
    // ResponsiveContainerのサイズ確定（ResizeObserverコールバック）を待つため非同期クエリを使う。
    expect(await screen.findByText("アラート境界")).toBeInTheDocument();
    expect(
      await screen.findByText("統計的期待レンジ（68%）"),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("統計的期待レンジ（95%）"),
    ).toBeInTheDocument();
  });

  it("shows the calculation basis note for the forecast range", () => {
    render(<StockChart data={baseData} />);
    expect(
      screen.getByText(/直近120営業日の対数リターンの平均と標準偏差/),
    ).toBeInTheDocument();
  });

  it("renders a marker on the JST calendar date, not the UTC calendar date, of the notification", async () => {
    // "2026-09-01T20:00:00Z" is 2026-09-02T05:00:00+09:00 in JST: UTC slice()
    // gives "2026-09-01" but the JST calendar date (which matches prices[].date,
    // a JST trading date) is "2026-09-02". `prices` below deliberately has NO
    // "2026-09-01" row — only "2026-09-02" — so if notificationByDate were keyed
    // by the naive UTC slice, the lookup key ("2026-09-01") would match no price
    // row at all and the marker would silently vanish. This is the regression
    // this test guards: with the JST fix, the marker renders on "2026-09-02";
    // without it, zero markers render.
    const data: StockChartData = {
      ...baseData,
      prices: [{ date: "2026-09-02", close: 3250.0 }],
      alert_band: [],
      notifications: [
        {
          notified_at: "2026-09-01T20:00:00Z",
          anomaly_score: 3.1,
          ai_report: "急騰の背景には...",
          slack_sent: true,
        },
      ],
    };

    const { container } = render(<StockChart data={data} />);
    // ResponsiveContainerのサイズ確定を待つ。
    await screen.findByText("終値");
    expect(container.querySelectorAll(".recharts-reference-dot").length).toBe(
      1,
    );
  });

  it("renders period toggle buttons with 6M selected by default", () => {
    render(<StockChart data={baseData} />);
    const group = screen.getByRole("group", { name: "表示期間" });
    const buttons = ["1M", "3M", "6M", "1Y", "2Y"].map((label) =>
      screen.getByRole("button", { name: label }),
    );
    buttons.forEach((button) => expect(group).toContainElement(button));

    const sixMonthButton = screen.getByRole("button", { name: "6M" });
    expect(sixMonthButton).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "1M" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("switches the selected period button when clicked", () => {
    render(<StockChart data={baseData} />);
    const oneMonthButton = screen.getByRole("button", { name: "1M" });

    fireEvent.click(oneMonthButton);

    expect(oneMonthButton).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "6M" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("actually changes the displayed chart data when the period is switched, not just the selected button", async () => {
    // 150件の連続する日次データを用意する。配列末尾（最新）から数えて:
    //   - 直近126件（6M, デフォルト）= index 24..149
    //   - 直近21件（1M）          = index 129..149
    // 通知はindex 50に置く。50は24以上・129未満なので「6Mには含まれるが
    // 1Mには含まれない」唯一の帯に入り、期間切り替えで表示/非表示が
    // 反転するはずの日付になる。もしstock-chart.tsxの該当行が
    // `sliceToDisplayWindow(data)`（第2引数なし＝常に6M固定）に後退していたら、
    // 1Mへの切り替え後もマーカーは消えず、このテストは失敗する。
    const prices = buildSequentialPrices(150);
    const notifiedDate = prices[50].date;
    const data: StockChartData = {
      ...baseData,
      prices,
      alert_band: [],
      notifications: [
        {
          notified_at: `${notifiedDate}T00:00:00Z`,
          anomaly_score: 3.1,
          ai_report: "急騰の背景には...",
          slack_sent: true,
        },
      ],
    };

    const { container } = render(<StockChart data={data} />);
    // ResponsiveContainerのサイズ確定を待つ。
    await screen.findByText("終値");

    // 6M（デフォルト）ではindex 50は直近126件の窓に入るのでマーカーが1件見える。
    expect(container.querySelectorAll(".recharts-reference-dot").length).toBe(
      1,
    );

    fireEvent.click(screen.getByRole("button", { name: "1M" }));

    // 1Mに切り替えるとindex 50は直近21件の窓から外れるのでマーカーは消える。
    expect(container.querySelectorAll(".recharts-reference-dot").length).toBe(
      0,
    );
  });

  it("keeps the forecast lines intact after switching periods", async () => {
    render(<StockChart data={baseData} />);
    fireEvent.click(screen.getByRole("button", { name: "1M" }));

    // 予測はもともとスライス対象外なので、期間切り替え後も凡例のラベルは残り続ける。
    expect(await screen.findByText("予測中心線")).toBeInTheDocument();
    expect(
      screen.queryByText("予測を取得できませんでした"),
    ).not.toBeInTheDocument();
  });
});

describe("sliceToDisplayWindow", () => {
  function buildPrices(n: number): StockChartPrice[] {
    return Array.from({ length: n }, (_, i) => ({
      date: `2026-01-${String((i % 28) + 1).padStart(2, "0")}`,
      close: 1000 + i,
    }));
  }

  function buildAlertBand(n: number): StockChartAlertBand[] {
    return Array.from({ length: n }, (_, i) => ({
      date: `2026-01-${String((i % 28) + 1).padStart(2, "0")}`,
      upper: 1100 + i,
      lower: 900 + i,
    }));
  }

  it("keeps all entries when at or under the display window size", () => {
    const data: StockChartData = {
      ...baseData,
      prices: buildPrices(100),
      alert_band: buildAlertBand(100),
    };
    const sliced = sliceToDisplayWindow(data);
    expect(sliced.prices).toHaveLength(100);
    expect(sliced.alert_band).toHaveLength(100);
  });

  it("slices prices and alert_band down to the display window when longer, keeping the most recent entries", () => {
    const prices = buildPrices(200);
    const alertBand = buildAlertBand(200);
    const data: StockChartData = { ...baseData, prices, alert_band: alertBand };

    const sliced = sliceToDisplayWindow(data);

    expect(sliced.prices.length).toBeLessThan(200);
    expect(sliced.prices).toHaveLength(126);
    expect(sliced.alert_band).toHaveLength(126);
    // 末尾（最新）が保持されている＝予測のアンカーになる最終価格は変わらない。
    expect(sliced.prices[sliced.prices.length - 1]).toEqual(
      prices[prices.length - 1],
    );
  });

  it("slices down to the given days argument, overriding the default window", () => {
    const prices = buildPrices(200);
    const alertBand = buildAlertBand(200);
    const data: StockChartData = { ...baseData, prices, alert_band: alertBand };

    const sliced = sliceToDisplayWindow(data, 21);

    expect(sliced.prices).toHaveLength(21);
    expect(sliced.alert_band).toHaveLength(21);
    expect(sliced.prices[sliced.prices.length - 1]).toEqual(
      prices[prices.length - 1],
    );
  });

  it("returns all entries when days exceeds the available history (e.g. a freshly added stock)", () => {
    const prices = buildPrices(10);
    const alertBand = buildAlertBand(10);
    const data: StockChartData = { ...baseData, prices, alert_band: alertBand };

    const sliced = sliceToDisplayWindow(data, PERIOD_TRADING_DAYS["2Y"]);

    expect(sliced.prices).toHaveLength(10);
    expect(sliced.alert_band).toHaveLength(10);
  });
});

function baseChartData(
  overrides: Partial<StockChartData> = {},
): StockChartData {
  return {
    stock_code: "7203",
    current_price: 3300,
    prices: [{ date: "2026-09-01", close: 3300 }],
    alert_band: [],
    current_z_score: null,
    forecast: {
      horizon: 20,
      points: [
        {
          step: 1,
          center: 3350,
          upper_68: 3400,
          lower_68: 3300,
          upper_95: 3450,
          lower_95: 3250,
        },
      ],
      direction_model: {
        adopted: false,
        predicted_direction: null,
        hit_rate: null,
        baseline_hit_rate: null,
        p_value: null,
        independent_sample_count: null,
        trained_at: null,
      },
    },
    notifications: [],
    ...overrides,
  };
}

describe("StockChart direction model badge", () => {
  it("adopted=falseの場合はバッジを表示しない", () => {
    render(<StockChart data={baseChartData()} />);
    expect(
      screen.queryByText(/AIモデルによる方向予測/),
    ).not.toBeInTheDocument();
  });

  it("adopted=trueの場合はバッジと方向ラベル・的中率を表示する（up）", () => {
    const data = baseChartData();
    data.forecast!.direction_model = {
      adopted: true,
      predicted_direction: "up",
      hit_rate: 0.57,
      baseline_hit_rate: 0.5,
      p_value: 0.01,
      independent_sample_count: 120,
      trained_at: "2026-09-29T00:00:00Z",
    };
    render(<StockChart data={data} />);
    expect(screen.getByText(/AIモデルによる方向予測/)).toBeInTheDocument();
    expect(screen.getByText(/57%/)).toBeInTheDocument();
    expect(screen.getByText(/将来の的中を保証しない/)).toBeInTheDocument();
    const upLabel = screen.getByText(/上昇\(強気\)/);
    expect(upLabel).toBeInTheDocument();
    expect(upLabel).toHaveTextContent("↑");
    expect(upLabel).toHaveClass("text-green-600");
  });

  it("adopted=trueの場合はバッジと方向ラベル・的中率を表示する（down）", () => {
    const data = baseChartData();
    data.forecast!.direction_model = {
      adopted: true,
      predicted_direction: "down",
      hit_rate: 0.6,
      baseline_hit_rate: 0.5,
      p_value: 0.02,
      independent_sample_count: 130,
      trained_at: "2026-09-29T00:00:00Z",
    };
    render(<StockChart data={data} />);
    expect(screen.getByText(/AIモデルによる方向予測/)).toBeInTheDocument();
    expect(screen.getByText(/60%/)).toBeInTheDocument();
    const downLabel = screen.getByText(/下落\(弱気\)/);
    expect(downLabel).toBeInTheDocument();
    expect(downLabel).toHaveTextContent("↓");
    expect(downLabel).toHaveClass("text-red-600");
  });

  it("adopted=trueでもpredicted_directionがnullならバッジ自体を表示しない", () => {
    const data = baseChartData();
    data.forecast!.direction_model = {
      adopted: true,
      predicted_direction: null,
      hit_rate: 0.55,
      baseline_hit_rate: 0.5,
      p_value: 0.03,
      independent_sample_count: 100,
      trained_at: "2026-09-29T00:00:00Z",
    };
    render(<StockChart data={data} />);
    expect(
      screen.queryByText(/AIモデルによる方向予測/),
    ).not.toBeInTheDocument();
  });

  it("adopted=trueでもhit_rateがnullならバッジを表示しない", () => {
    // go-api側の修正でこの状態には到達しなくなったはずだが、UIが「的中率0%、
    // 統計的に有意」という内部矛盾した表示をしないことを多層防御として保証する。
    const data = baseChartData();
    data.forecast!.direction_model = {
      adopted: true,
      predicted_direction: "up",
      hit_rate: null,
      baseline_hit_rate: null,
      p_value: null,
      independent_sample_count: null,
      trained_at: null,
    };
    render(<StockChart data={data} />);
    expect(
      screen.queryByText(/AIモデルによる方向予測/),
    ).not.toBeInTheDocument();
  });

  it("forecastがnullでもクラッシュしない", () => {
    render(<StockChart data={baseChartData({ forecast: null })} />);
    expect(screen.getByText("予測を取得できませんでした")).toBeInTheDocument();
  });
});
