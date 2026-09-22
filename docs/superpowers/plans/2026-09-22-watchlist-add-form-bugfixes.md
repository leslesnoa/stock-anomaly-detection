# Watchlistフォーム不具合修正 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ユーザーから報告された2件の不具合を修正する。(1) 証券コードのバリデーションが数字4桁のみを許可しており、東証が2024年から導入した英字混在コード(例: `421A`)を弾いてしまう。(2) watchlist追加フォームでバリデーションエラーが表示されると、証券コード入力欄が極端に狭くなり入力できなくなる。

**Architecture:** (1) は `go-api/internal/domain/stock/value_object.go` の正規表現を「先頭3桁は数字、4桁目は数字または英大文字」に緩和する(ドメイン層の値オブジェクトのみの変更、呼び出し側のシグネチャは変わらない)。(2) は `frontend/components/add-watchlist-form.tsx` のフォームコンテナに `flex-wrap` を追加し、`basis-full` のエラーメッセージが入力欄・ボタンを圧迫せず独立した行に折り返されるようにする(共有UIコンポーネント `components/ui/input.tsx` は変更しない)。

**Tech Stack:** Go 1.x (go-api, 標準ライブラリ `regexp` + testify)、Next.js/React (frontend, Tailwind CSS)

**Spec:** なし(ユーザーからの不具合報告2件がそのままスコープ。事前のspec文書は作成していない)

## Global Constraints

- go-api: `go test -race -short ./...` がグリーンであること(`go-api/`ディレクトリから実行)
- go-api: 既存のエラーメッセージは英語表記の慣習に従う(`internal/domain/*/*.go` の既存 `errors.New(...)` 参照)
- frontend: 共有UIプリミティブ `components/ui/input.tsx` の `min-w-0` は他の全フォームに影響するため変更しない。修正は `add-watchlist-form.tsx` 内に閉じる
- 修正後は実際にdevサーバーを起動しブラウザで両方の不具合が解消したことを目視確認する

---

### Task 1: 証券コードバリデーションの正規表現を新形式に対応させる

**Files:**
- Modify: `go-api/internal/domain/stock/value_object.go:10-12`
- Test: `go-api/internal/domain/stock/value_object_test.go`

**Interfaces:**
- Consumes: なし(独立した値オブジェクト)
- Produces: `stock.NewStockCode(code string) (StockCode, error)` のシグネチャは変更しない。`stock.ErrInvalidStockCode` の変数名も変更しない(呼び出し元 `go-api/internal/interface/handler/watchlist_handler.go:80` の `errors.Is` 判定はそのまま使える)

- [ ] **Step 1: 失敗するテストケースを追加する**

`go-api/internal/domain/stock/value_object_test.go` の `tests` スライスに以下のケースを追加する(既存の4件はそのまま残す):

```go
		{"valid alphanumeric code (new TSE format)", "421A", false},
		{"lowercase letter suffix returns error", "421a", true},
		{"letter not in last position returns error", "42A1", true},
		{"two letter suffix returns error", "42AA", true},
```

- [ ] **Step 2: テストを実行し、新規ケースが失敗することを確認する**

Run: `cd go-api && go test ./internal/domain/stock/... -run TestNewStockCode -v`
Expected: `valid alphanumeric code (new TSE format)` が `FAIL`(現行正規表現 `^\d{4}$` は英字を許可しないため)。他の新規ケース(`421a`, `42A1`, `42AA`)は現行正規表現でもすでに拒否されるため `PASS` のままでよい。

- [ ] **Step 3: 正規表現とエラーメッセージを更新する**

`go-api/internal/domain/stock/value_object.go:10-12` を以下に変更する:

```go
var stockCodeRegexp = regexp.MustCompile(`^[0-9]{3}[0-9A-Z]$`)

var ErrInvalidStockCode = errors.New("stock code must be 4 characters: 3 digits followed by a digit or an uppercase letter")
```

(先頭3桁は数字固定、4桁目のみ数字または英大文字を許可する。東証の新コード体系は数字が枯渇した銘柄の末尾1桁のみを英字に置き換える方式のため、この位置制約で十分。)

- [ ] **Step 4: テストを再実行し、全ケースがパスすることを確認する**

Run: `cd go-api && go test ./internal/domain/stock/... -run TestNewStockCode -v`
Expected: 全ケース `PASS`

- [ ] **Step 5: パッケージ全体とrace検出込みの単体テストを実行する**

Run: `cd go-api && go test -race -short ./...`
Expected: 全パッケージ `ok`(既存のハンドラ/ユースケーステストは `stock.NewStockCode("7203")` などを使っているため影響を受けない)

- [ ] **Step 6: コミット**

```bash
git add go-api/internal/domain/stock/value_object.go go-api/internal/domain/stock/value_object_test.go
git commit -m "fix: allow alphanumeric stock codes (TSE new format e.g. 421A)

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 2: watchlist追加フォームのエラー表示時レイアウト崩れを修正する

**Files:**
- Modify: `frontend/components/add-watchlist-form.tsx:19-22`

**Interfaces:**
- Consumes: なし(コンポーネント内で閉じたCSS変更)
- Produces: なし

**背景:** `sm`(640px)以上では `form` が `flex-row`・`nowrap`(デフォルト)になる。入力欄ラッパー(`flex-1` = `flex-basis:0%`)とボタン(`flex-basis:auto`)がまず行の幅を使い切り、その後にエラーメッセージの `<p sm:basis-full>` が同じ行に押し込まれようとするため、`min-w-0` が指定された入力欄が極端に縮む。`flex-wrap` を有効にすれば、`basis-full` のエラー行は残り幅が無い1行目に収まらず自動的に2行目へ折り返され、1行目の入力欄・ボタンは通常サイズを維持できる。

- [ ] **Step 1: 現状のレイアウト崩れを再現して確認する(手動)**

```bash
cd frontend && npm run dev
```

ブラウザで `http://localhost:3000/watchlist` を開き(ログインが必要な場合はログイン)、幅640px以上のウィンドウで証券コード欄に無効な値(例: `1`)を入力して送信し、エラーメッセージ表示時に入力欄が極端に狭くなることを確認する。

- [ ] **Step 2: フォームコンテナに `flex-wrap` を追加する**

`frontend/components/add-watchlist-form.tsx:19-22` を以下に変更する:

```tsx
    <form
      action={formAction}
      className="flex flex-col flex-wrap gap-3 sm:flex-row sm:items-end sm:gap-2"
    >
```

(`flex-wrap` はデフォルトの `flex-col`(縦積み)には影響しないが、`sm:flex-row` 適用時に `sm:basis-full` のエラー `<p>` を独立した行へ折り返すために必要)

- [ ] **Step 3: ブラウザで修正を確認する**

devサーバーが起動したままの状態で `http://localhost:3000/watchlist` をリロードし、幅640px以上で同じ手順(無効な証券コードを送信)を再現する。エラーメッセージが入力欄・ボタンの下に独立した行として表示され、入力欄とボタンのサイズが通常時と変わらないことを確認する。あわせて有効な証券コード(例: `7203`)・新形式コード(例: `421A`)でwatchlist追加が成功することも確認する。

- [ ] **Step 4: 既存のフロントエンドテストを実行する**

Run: `cd frontend && npm test`
Expected: 既存のVitestテストが全てパス(このコンポーネントに対する既存の自動テストはないため、新規テスト追加は不要。レイアウト確認はStep 3の手動確認が正)

- [ ] **Step 5: コミット**

```bash
git add frontend/components/add-watchlist-form.tsx
git commit -m "fix: prevent stock code input from collapsing when validation error wraps

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Self-Review

- **Spec coverage:** 不具合1(英数字コード拒否)→ Task 1、不具合2(入力欄の極端な縮小)→ Task 2。両方カバー済み。
- **Placeholder scan:** 全ステップに実コード・実コマンドを記載済み。TBD等のプレースホルダーなし。
- **Type consistency:** `stock.NewStockCode` のシグネチャ・`ErrInvalidStockCode` 変数名は変更せず、既存呼び出し元(`manage_watchlist.go:31`, `watchlist_handler.go:80`)との整合性を維持。
