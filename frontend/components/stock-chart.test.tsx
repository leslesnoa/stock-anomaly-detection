import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { render, screen } from "@testing-library/react";
import { StockChart } from "./stock-chart";
import type { StockChart as StockChartData } from "@/lib/go-api-client";

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
});
