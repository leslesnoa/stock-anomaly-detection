# 証券コード正規化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** watchlist追加時の証券コード入力を、バリデーション(`stock.NewStockCode`)に渡す前に前後の空白除去・大文字化する。これにより、東証新コード体系(4桁目英字、例: `421A`)で現実的に起こりうる小文字入力(`421a`)や前後空白付き入力(` 421A `)が不必要に400エラーになる問題を解消する。

**Architecture:** `go-api/internal/usecase/manage_watchlist.go` の `Add` メソッド冒頭、`stock.NewStockCode(rawStockCode)` を呼ぶ直前で `strings.ToUpper(strings.TrimSpace(rawStockCode))` を適用する1行の変更。ドメイン層の `StockCode` 値オブジェクト自体は厳格なバリデーションを維持し(呼び出し元によって挙動が変わらないようにするため)、正規化はusecase境界(ユーザー入力の受け口)に閉じる。

**Tech Stack:** Go 1.x (go-api, 標準ライブラリ `strings` + testify)

**Spec:** なし(既存メモリ `project_followup_stock_code_normalization` に記録済みのフォローアップ事項をそのまま実装する)

## Global Constraints

- go-api: `go test -race -short ./...` がグリーンであること(`go-api/`ディレクトリから実行)
- `ManageWatchlistUsecase.Add` のシグネチャは変更しない
- ドメイン層 `stock.NewStockCode` の正規表現・バリデーション挙動自体は変更しない(正規化は呼び出し側でのみ行う)

---

### Task 1: usecase境界で証券コードを正規化する

**Files:**
- Modify: `go-api/internal/usecase/manage_watchlist.go:30-34`
- Test: `go-api/internal/usecase/manage_watchlist_test.go`

**Interfaces:**
- Consumes: `stock.NewStockCode(code string) (StockCode, error)`(変更なし)
- Produces: `ManageWatchlistUsecase.Add` のシグネチャ・挙動は「正規化後に既存と同じバリデーションを通す」以外変更なし

- [ ] **Step 1: 失敗するテストケースを追加する**

`go-api/internal/usecase/manage_watchlist_test.go` に以下のテストを追加する(既存の `TestManageWatchlistUsecase_Add_Success` 等と同じ形式):

```go
func TestManageWatchlistUsecase_Add_NormalizesLowercaseAndWhitespace(t *testing.T) {
	repo := new(mockWatchlistRepository)
	code, _ := stock.NewStockCode("421A")
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 2.5}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 2.5}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nil)
	result, err := uc.Add(context.Background(), "user-1", "  421a  ", 0)

	require.NoError(t, err)
	require.Equal(t, "wl-1", result.ID)
	repo.AssertExpectations(t)
}
```

- [ ] **Step 2: テストを実行し、失敗することを確認する**

Run: `cd go-api && go test ./internal/usecase/... -run TestManageWatchlistUsecase_Add_NormalizesLowercaseAndWhitespace -v`
Expected: `FAIL`(`"  421a  "` は正規化前は `stock.NewStockCode` の `^[0-9]{3}[0-9A-Z]$` にマッチせず `ErrInvalidStockCode` を返すため、`repo.On("Create", ...)` が呼ばれず `mock.AssertExpectations` が失敗する)

- [ ] **Step 3: `manage_watchlist.go` に正規化を実装する**

`go-api/internal/usecase/manage_watchlist.go` の import に `"strings"` を追加し、`Add` メソッド冒頭を以下に変更する:

```go
func (u *ManageWatchlistUsecase) Add(ctx context.Context, userID, rawStockCode string, threshold float64) (watchlist.Watchlist, error) {
	code, err := stock.NewStockCode(strings.ToUpper(strings.TrimSpace(rawStockCode)))
	if err != nil {
		return watchlist.Watchlist{}, err
	}
```

- [ ] **Step 4: テストを再実行し、パスすることを確認する**

Run: `cd go-api && go test ./internal/usecase/... -run TestManageWatchlistUsecase_Add -v`
Expected: 全ケース(既存の `Success`, `DefaultThreshold`, `InvalidStockCode` 等 + 新規 `NormalizesLowercaseAndWhitespace`)が `PASS`

- [ ] **Step 5: パッケージ全体とrace検出込みの単体テストを実行する**

Run: `cd go-api && go test -race -short ./...`
Expected: 全パッケージ `ok`

- [ ] **Step 6: コミット**

```bash
git add go-api/internal/usecase/manage_watchlist.go go-api/internal/usecase/manage_watchlist_test.go
git commit -m "fix: normalize stock code case and whitespace before validation

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Self-Review

- **Spec coverage:** フォローアップ事項(証券コード入力の大文字小文字・空白正規化)をそのままカバー。
- **Placeholder scan:** 全ステップに実コード・実コマンドを記載済み。
- **Type consistency:** `ManageWatchlistUsecase.Add` のシグネチャ・`stock.NewStockCode` のシグネチャともに変更なし。
