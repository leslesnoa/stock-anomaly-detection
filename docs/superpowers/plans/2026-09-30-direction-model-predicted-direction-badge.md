# AI方向分類器 predicted_direction 明示表示 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `frontend/components/stock-chart.tsx` の方向分類器バッジに、既に取得済みの `direction_model.predicted_direction`（"up" | "down" | null）を、矢印・色分け付きの明示的な日本語ラベルとして表示する。

**Architecture:** `predicted_direction` を表示用ラベル（テキスト・矢印・Tailwindの色クラス）にマッピングする純粋関数 `directionLabel()` を `stock-chart.tsx` 内に追加し、既存の的中率アナウンス `<p>`（`directionModel?.adopted && directionModel.hit_rate != null` の条件ブロック）の先頭にラベルの `<span>` を挿入する。`predicted_direction` が `null` の場合はラベルを省略し、既存の的中率文のみ表示する（多層防御・現状のgo-api実装では到達しない想定の分岐）。

**Tech Stack:** Next.js (React) + TypeScript、Vitest + @testing-library/react（フロントエンドの既存テストスタック）。新規依存追加なし。

**Spec:** このタスクは会話内でのbounded設計承認（brainstorming skillのbounded path）に基づく。設計要点はGoalに記載の通り。別途スペックファイルは作成していない。

## Global Constraints

- 既存の的中率アナウンス文言・的中率テストは変更・削除しない（既存3テストは全てパスし続けること）
- 新規依存パッケージは追加しない
- `predicted_direction` が `null` のときにラベル欄が空文字列や `undefined` にならず、既存の的中率文がそのまま表示されること（多層防御を崩さない）
- 色はTailwindユーティリティクラス（`text-green-600` / `text-red-600`）を直接使う。新しいCSS変数やデザイントークンは追加しない

## Review Focus

- `predicted_direction: null` かつ `adopted: true` かつ `hit_rate` が非null（型上は許容されるがgo-api実装では未到達）の場合に、ラベルなしで的中率文のみが表示され、クラッシュしないこと
- `predicted_direction: "down"` のケースが `"up"` と同様に正しいラベル・矢印・色クラスで表示されること
- 既存の3テスト（バッジ非表示・バッジ表示・hit_rate=nullで非表示）が壊れていないこと
- ラベルの矢印文字（↑/↓）がスクリーンリーダー等で意味不明な記号読み上げにならないよう、隣接するテキストラベル（「上昇(強気)」「下落(弱気)」）が常に併記されること
- `directionLabel()` が `stock-chart.tsx` の他のロジック（`buildRows` 等）に影響を与えない独立した純粋関数として実装されていること

---

### Task 1: predicted_direction のラベル表示を追加する

**Files:**
- Modify: `frontend/components/stock-chart.tsx:1-20`（型インポートは変更不要、既存の `StockChartData` 等のインポートのみ利用）
- Modify: `frontend/components/stock-chart.tsx:246-253`（既存の的中率アナウンスブロック）
- Test: `frontend/components/stock-chart.test.tsx:400-434`（既存の2テストを拡張、新規テストを追加）

**Interfaces:**
- Consumes: `data.forecast.direction_model.predicted_direction`（型 `"up" | "down" | null"`、`frontend/lib/go-api-client.ts:126` で定義済み、変更なし）
- Produces: `directionLabel(direction: "up" | "down" | null): { text: string; arrow: string; colorClass: string } | null` — `stock-chart.tsx` 内のモジュールレベル関数。`null` を渡すと `null` を返す。他ファイルからは呼ばれない（exportしない）。

- [ ] **Step 1: 失敗するテストを書く（"up"ケースの拡張 + "down"ケース新規追加 + null多層防御ケース）**

`frontend/components/stock-chart.test.tsx` の398行目直後（`it("adopted=trueの場合は...")` テストの中）を以下のように書き換える。まず既存の400-415行目のテストを置き換える:

```typescript
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

  it("adopted=trueでもpredicted_directionがnullなら方向ラベルを出さず的中率文のみ表示する", () => {
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
    expect(screen.getByText(/AIモデルによる方向予測/)).toBeInTheDocument();
    expect(screen.queryByText(/上昇\(強気\)/)).not.toBeInTheDocument();
    expect(screen.queryByText(/下落\(弱気\)/)).not.toBeInTheDocument();
  });
```

（417-434行目の既存「hit_rateがnullならバッジを表示しない」テストはそのまま残す。）

- [ ] **Step 2: テストを実行し、失敗することを確認する**

Run: `cd frontend && npx vitest run components/stock-chart.test.tsx`
Expected: 新規/拡張した3テストがFAIL（`上昇(強気)` や `下落(弱気)` のテキストが見つからない）。既存の他テストはPASSのまま。

- [ ] **Step 3: `directionLabel` 関数を実装し、表示ブロックに組み込む**

`frontend/components/stock-chart.tsx` の156行目（`export function StockChart` の直前）に以下の関数を追加する:

```typescript
function directionLabel(
  direction: "up" | "down" | null,
): { text: string; arrow: string; colorClass: string } | null {
  if (direction === "up") {
    return { text: "上昇(強気)", arrow: "↑", colorClass: "text-green-600" };
  }
  if (direction === "down") {
    return { text: "下落(弱気)", arrow: "↓", colorClass: "text-red-600" };
  }
  return null;
}
```

246-253行目の既存ブロックを以下に置き換える:

```typescript
      {directionModel?.adopted && directionModel.hit_rate != null && (
        <p className="text-xs text-muted-foreground" role="status">
          AIモデルによる方向予測:{" "}
          {(() => {
            const label = directionLabel(directionModel.predicted_direction);
            return label ? (
              <span className={`font-semibold ${label.colorClass}`}>
                {label.arrow} {label.text}
              </span>
            ) : null;
          })()}
          （過去データでの的中率
          {Math.round((directionModel.hit_rate ?? 0) * 100)}
          %、既存手法比で統計的に有意）。
          この的中率は過去データでの検証結果であり、将来の的中を保証しないことにご注意ください。
        </p>
      )}
```

- [ ] **Step 4: テストを実行し、全てパスすることを確認する**

Run: `cd frontend && npx vitest run components/stock-chart.test.tsx`
Expected: 全テストPASS（既存の的中率バッジ関連テスト含む）。

- [ ] **Step 5: 型チェックとlintを実行する**

Run: `cd frontend && npx tsc --noEmit && npx eslint components/stock-chart.tsx components/stock-chart.test.tsx`
Expected: エラーなし。

- [ ] **Step 6: コミットする**

```bash
git add frontend/components/stock-chart.tsx frontend/components/stock-chart.test.tsx
git commit -m "$(cat <<'EOF'
feat(frontend): predicted_directionを方向ラベルとして明示表示

AI方向分類器のpredicted_direction(up/down)がこれまでUI上に
テキスト・矢印・色分けとして表示されておらず、ユーザーが
予測中心線の傾きから間接的に読み取るしかなかった。
directionLabel()でup/downを矢印付き日本語ラベル（緑/赤）に
マッピングし、既存の的中率アナウンスの先頭に表示する。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review Notes

- **Spec coverage:** ラベル文言・矢印・色分け・null時の多層防御・テスト拡張、すべて Task 1 でカバー済み。単一の小さな変更のため追加タスク分割は不要。
- **Placeholder scan:** なし。全ステップに実コードを記載済み。
- **Type consistency:** `directionLabel` の引数型は `StockChartDirectionModel.predicted_direction`（`frontend/lib/go-api-client.ts:126`）とそのまま一致。戻り値の `colorClass` はJSX側でテンプレートリテラルとして使用。
- **Review Focus:** 上記5項目はいずれもTask 1のテスト（"up"/"down"/null多層防御ケース、既存3テストの維持）でカバー済み。
