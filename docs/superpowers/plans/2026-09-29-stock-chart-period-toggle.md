# 表示期間切替UI（1M/3M/6M/1Y/2Y）実装プラン

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `StockChart` コンポーネントに 1M/3M/6M/1Y/2Y の表示期間切り替えボタンを追加し、選択した期間に応じてチャートに表示する価格・アラート境界の件数をクライアント側で絞り込めるようにする。

**Architecture:** `frontend/components/stock-chart.tsx` の既存関数 `sliceToDisplayWindow` が持つ固定値 `DISPLAY_TRADING_DAYS`（126営業日固定）を、期間ごとの営業日数テーブルとuseStateによる選択状態に置き換える。バックエンド（Go API `/stocks/{code}/chart`）は既に最大2年分（`backfillDays=500`営業日）のデータを返しているため、API呼び出しやGoコードの変更は不要。予測（forecast）はもともとスライス対象外であり、期間切り替えの影響を受けない。

**Tech Stack:** Next.js（App Router）, React（`useState`）, Recharts, 既存の `Button` UIコンポーネント（`@/components/ui/button`）, Vitest + Testing Library

**Spec:** このプランはチャット上でユーザーと合意した簡潔な設計（bounded path、書面のspecファイルなし）に基づく。合意内容: (1) 期間ごとの営業日数は 1M=21 / 3M=63 / 6M=126（現状のデフォルトを維持） / 1Y=252 / 2Y=500（バックフィル上限と一致）。(2) `sliceToDisplayWindow` にdays引数を追加し、デフォルト値で既存の呼び出し・テストとの互換を保つ。(3) ボタンはチャート上部に横並びで配置し、選択中の期間を `variant="default"`、非選択を `variant="outline"` で視覚的に区別する。(4) API・Goバックエンドの変更は無し。

## Global Constraints

- 追加のAPI呼び出し・Goバックエンド変更は行わない（既存の `fetchStockChart` が返すデータのみで完結させる）
- `sliceToDisplayWindow` の既存シグネチャ利用箇所（デフォルト6M=126）との後方互換を壊さない
- 予測（`forecast`）のアンカー（実績終値の最終点）や予測点はスライスの影響を受けてはならない（既存の不変条件を維持）
- UIボタンは既存の `Button`（`@/components/ui/button`）を使い、独自のボタン実装をしない

## Review Focus

- 期間を切り替えた際、予測線（forecastCenter/forecast68Range/forecast95Range）や予測アンカー（実績終値の最終点）が変化しない、または消えないこと（forecastはスライス対象外という既存不変条件の回帰確認）
- 選択した期間の営業日数よりも実際の `prices` 件数が少ない場合（例: 新規watchlist追加直後で数日分しかない銘柄で2Yを選択）でもクラッシュせず、全件表示されること（`Array.prototype.slice` の負数超過は自然に全件を返すため要件としては満たすが、テストで明示する）
- ボタンの選択状態がaria属性（`aria-pressed`）で機視化されており、スクリーンリーダー利用者にも現在の選択が伝わること
- 期間切り替えボタンをクリックしても、既存のツールチップ・凡例・アラート境界などのラベル表示に副作用が出ないこと（既存テストの回帰確認）
- デフォルト選択が6M（従来の固定表示と同じ126営業日）であること、既存ユーザーから見て挙動が変わらないこと

---

## Task 1: `sliceToDisplayWindow` をdays引数でパラメータ化する

**Files:**
- Modify: `frontend/components/stock-chart.tsx:19-33`
- Test: `frontend/components/stock-chart.test.tsx:176-218`（既存の `sliceToDisplayWindow` テスト群）

**Interfaces:**
- Consumes: なし（このタスクは既存コードの拡張のみ）
- Produces:
  - `export type ChartPeriod = "1M" | "3M" | "6M" | "1Y" | "2Y";`
  - `export const PERIOD_TRADING_DAYS: Record<ChartPeriod, number>`（`{ "1M": 21, "3M": 63, "6M": 126, "1Y": 252, "2Y": 500 }`）
  - `export const PERIOD_OPTIONS: ChartPeriod[]`（`["1M", "3M", "6M", "1Y", "2Y"]`、この順序でUIに表示する）
  - `export const DEFAULT_PERIOD: ChartPeriod = "6M";`
  - `export function sliceToDisplayWindow(data: StockChartData, days?: number): StockChartData`（`days` 省略時は `PERIOD_TRADING_DAYS[DEFAULT_PERIOD]`＝126を使う。Task 2はこの関数に `PERIOD_TRADING_DAYS[period]` を明示的に渡す）

- [ ] **Step 1: 失敗するテストを書く（1Mの営業日数で絞り込まれること）**

`frontend/components/stock-chart.test.tsx` の `describe("sliceToDisplayWindow", ...)` ブロック内、既存の2つの `it` の後に追加:

```typescript
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
```

ファイル先頭のimportに `PERIOD_TRADING_DAYS` を追加:

```typescript
import { StockChart, sliceToDisplayWindow, PERIOD_TRADING_DAYS } from "./stock-chart";
```

- [ ] **Step 2: テストを実行して失敗を確認する**

Run: `cd frontend && npx vitest run stock-chart.test.tsx`
Expected: FAIL — `PERIOD_TRADING_DAYS` が存在せずimportエラー、または `sliceToDisplayWindow` が2引数目を無視して126固定のまま返すため件数不一致で失敗する。

- [ ] **Step 3: `sliceToDisplayWindow` をパラメータ化する**

`frontend/components/stock-chart.tsx:19-33` を以下に置き換える:

```typescript
export type ChartPeriod = "1M" | "3M" | "6M" | "1Y" | "2Y";

// 各期間ボタンに対応する営業日数。1ヶ月≒21営業日で概算。2Yはバックフィル上限
// （backfillDays=500営業日、go-api側 usecase.BackfillPriceHistoryUsecase）と一致させ、
// 取得済みデータの実質フルレンジを表す。
export const PERIOD_TRADING_DAYS: Record<ChartPeriod, number> = {
  "1M": 21,
  "3M": 63,
  "6M": 126,
  "1Y": 252,
  "2Y": 500,
};

export const PERIOD_OPTIONS: ChartPeriod[] = ["1M", "3M", "6M", "1Y", "2Y"];

export const DEFAULT_PERIOD: ChartPeriod = "6M";

// 表示用に直近days件へ絞り込む。予測の起点（最新の価格・日付）は
// 絞り込み後も変わらない（sliceは末尾を保持するため）ので、buildRowsの予測アンカリングには
// 影響しない。notificationsは絞り込まない: markerRows は絞り込み後のrows（data.prices由来）
// に存在する日付としか一致しないため、絞り込みは自然に反映される。
// daysが実際の件数を超える場合（新規watchlist登録直後などhistoryが浅い場合）は
// Array.prototype.sliceの性質上、全件がそのまま返る。
export function sliceToDisplayWindow(
  data: StockChartData,
  days: number = PERIOD_TRADING_DAYS[DEFAULT_PERIOD],
): StockChartData {
  return {
    ...data,
    prices: data.prices.slice(-days),
    alert_band: data.alert_band.slice(-days),
  };
}
```

- [ ] **Step 4: テストを実行して成功を確認する**

Run: `cd frontend && npx vitest run stock-chart.test.tsx`
Expected: PASS — Task 2着手前なので `StockChart` 自体の既存テスト（デフォルト6M=126を暗黙に使う）も含めて全件PASSする。

- [ ] **Step 5: コミット**

```bash
cd frontend && git add components/stock-chart.tsx components/stock-chart.test.tsx
git commit -m "feat(frontend): parameterize sliceToDisplayWindow with period trading-day counts"
```

---

## Task 2: 期間切り替えボタンUIを追加し `StockChart` に状態を持たせる

**Files:**
- Modify: `frontend/components/stock-chart.tsx`（`StockChart` 関数内、importにも `useState` と `Button` を追加）
- Test: `frontend/components/stock-chart.test.tsx`

**Interfaces:**
- Consumes: Task 1で作った `PERIOD_TRADING_DAYS`, `PERIOD_OPTIONS`, `DEFAULT_PERIOD`, `ChartPeriod`, `sliceToDisplayWindow(data, days)`
- Produces: `StockChart` コンポーネントのUIに期間切り替えボタン群（`role="group"`, `aria-label="表示期間"`、各ボタンは `aria-pressed` で選択状態を示す）。このタスクが最終タスクのため、後続タスクへの新規インターフェース提供はなし。

- [ ] **Step 1: 失敗するテストを書く（ボタンが表示され、クリックで選択状態が切り替わること）**

`frontend/components/stock-chart.test.tsx` の `describe("StockChart", ...)` ブロック内、既存の最後の `it`（JST日付マーカーのテスト）の後に追加。ファイル先頭のimportに `fireEvent` を追加する必要がある:

```typescript
import { render, screen, fireEvent } from "@testing-library/react";
```

追加するテスト:

```typescript
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

  it("keeps the forecast lines intact after switching periods", async () => {
    render(<StockChart data={baseData} />);
    fireEvent.click(screen.getByRole("button", { name: "1M" }));

    // 予測はもともとスライス対象外なので、期間切り替え後も凡例のラベルは残り続ける。
    expect(await screen.findByText("予測中心線")).toBeInTheDocument();
    expect(
      screen.queryByText("予測を取得できませんでした"),
    ).not.toBeInTheDocument();
  });
```

- [ ] **Step 2: テストを実行して失敗を確認する**

Run: `cd frontend && npx vitest run stock-chart.test.tsx`
Expected: FAIL — `role="group"` の要素も期間ボタンも存在しないため `getByRole` が見つからずエラーになる。

- [ ] **Step 3: `StockChart` に期間切り替えUIを実装する**

`frontend/components/stock-chart.tsx` の先頭のimportに `useState` を追加:

```typescript
"use client";

import { useState } from "react";
import {
  ComposedChart,
  Area,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ReferenceDot,
} from "recharts";
import { Button } from "@/components/ui/button";
import type {
  StockChart as StockChartData,
  StockChartNotification,
} from "@/lib/go-api-client";
```

`export function StockChart({ data }: { data: StockChartData })` の本体を以下に置き換える:

```typescript
export function StockChart({ data }: { data: StockChartData }) {
  const [period, setPeriod] = useState<ChartPeriod>(DEFAULT_PERIOD);
  // 表示は選択中の期間に絞る。sliceは末尾（最新）を保持するため、予測のアンカー
  // （data.prices の最終要素の日付・終値）は絞り込み前後で変わらない。
  const displayData = sliceToDisplayWindow(data, PERIOD_TRADING_DAYS[period]);
  const rows = buildRows(displayData);
  const markerRows = rows.filter(
    (r) => r.notification !== undefined && r.close !== undefined,
  );

  return (
    <div className="space-y-2">
      <div className="flex gap-1" role="group" aria-label="表示期間">
        {PERIOD_OPTIONS.map((p) => (
          <Button
            key={p}
            type="button"
            size="xs"
            variant={p === period ? "default" : "outline"}
            aria-pressed={p === period}
            onClick={() => setPeriod(p)}
          >
            {p}
          </Button>
        ))}
      </div>
      {!data.forecast && (
        <p className="text-sm text-muted-foreground" role="status">
          予測を取得できませんでした
        </p>
      )}
      <ResponsiveContainer width="100%" height={400}>
        <ComposedChart data={rows}>
          <XAxis dataKey="date" />
          <YAxis domain={["auto", "auto"]} />
          <Tooltip content={<ChartTooltip />} />
          <Legend />
          <Area
            dataKey="alertRange"
            stroke="none"
            fill="#f59e0b"
            fillOpacity={0.15}
            name="アラート境界"
            isAnimationActive={false}
          />
          <Area
            dataKey="forecast95Range"
            stroke="none"
            fill="#6366f1"
            fillOpacity={0.08}
            name="統計的期待レンジ（95%）"
            isAnimationActive={false}
          />
          <Area
            dataKey="forecast68Range"
            stroke="none"
            fill="#6366f1"
            fillOpacity={0.15}
            name="統計的期待レンジ（68%）"
            isAnimationActive={false}
          />
          <Line
            dataKey="close"
            stroke="#0f172a"
            dot={false}
            name="終値"
            isAnimationActive={false}
          />
          <Line
            dataKey="forecastCenter"
            stroke="#6366f1"
            strokeDasharray="4 4"
            dot={false}
            name="予測中心線"
            isAnimationActive={false}
          />
          {markerRows.map((r) => (
            <ReferenceDot
              key={r.date}
              x={r.date}
              y={r.close}
              r={5}
              fill="#ef4444"
              stroke="none"
            />
          ))}
        </ComposedChart>
      </ResponsiveContainer>
      <p className="text-xs text-muted-foreground">
        統計的期待レンジは直近120営業日の対数リターンの平均と標準偏差から算出した
        ドリフト＋ボラティリティ区間であり、価格予測ではありません。アラート境界は
        直近30日の平均±アラート閾値σ（実際に通知が発火する境界）を示します。
      </p>
    </div>
  );
}
```

- [ ] **Step 4: テストを実行して成功を確認する**

Run: `cd frontend && npx vitest run stock-chart.test.tsx`
Expected: PASS — 全テスト（既存分含む）が通ること。

- [ ] **Step 5: フロントエンド全体のテストと型チェック・lintを実行する**

Run:
```bash
cd frontend
npx vitest run
npx tsc --noEmit
npm run lint
```
Expected: すべて成功（既存の他コンポーネントのテストに影響がないことを確認）。

- [ ] **Step 6: コミット**

```bash
cd frontend && git add components/stock-chart.tsx components/stock-chart.test.tsx
git commit -m "feat(frontend): add 1M/3M/6M/1Y/2Y period toggle buttons to StockChart"
```
