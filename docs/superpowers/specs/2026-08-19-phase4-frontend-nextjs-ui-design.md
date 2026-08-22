# Phase 4 フロントエンド: Next.js UI 設計書

## 1. 背景・スコープ

設計書 `docs/superpowers/specs/2026-07-05-stock-anomaly-detection-design.md` の
Phase 4「ユーザー管理・JWT認証・Next.js UI」は、バックエンド（PR #7、マージ済み）と
フロントエンドの2サブプロジェクトに分割することで合意済み（`docs/superpowers/specs/2026-08-14-phase4-user-auth-watchlist-api-design.md` 参照）。
本設計はフロントエンド部分のみを対象とする。

利用可能なバックエンドAPI（すべて`go-api`、Phase 4バックエンドで実装済み）:
- `POST /auth/register` — ユーザー登録
- `POST /auth/login` — ログイン（JWT発行、有効期限24h）
- `GET /watchlist` — 保有銘柄一覧（JWT認証必須）
- `POST /watchlist` — 銘柄追加（JWT認証必須）
- `DELETE /watchlist/{id}` — 銘柄削除（JWT認証必須）
- `PATCH /watchlist/{id}` — 閾値変更（JWT認証必須）

**スコープに含むもの**:
- ユーザー登録・ログイン画面
- watchlist一覧・追加・削除・閾値変更画面
- JWTのhttpOnly Cookie管理（ログイン状態の保持）

**スコープに含まないもの（フォローアップ）**:
- 通知履歴閲覧 — 対応するバックエンドAPI（`GET /notifications`等）が未実装のため。バックエンドAPI追加とセットで別サブプロジェクトとする
- Vercelへの実デプロイ作業 — ローカル開発・テストまでを今回のスコープとし、実デプロイはGo API側の本番環境変数整備と合わせて後日行う
- E2Eテスト（Playwright等） — 単体テストのみを今回のスコープとし、E2Eは後日追加（[[project_frontend_followup_e2e_testing]]としてmemory記録済み）
- プロフィール編集・パスワード変更 — 元設計書にも記載がなくスコープ外

## 2. 技術スタック

| レイヤー | 技術 | 理由 |
|---|---|---|
| フレームワーク | Next.js（App Router）+ TypeScript | 元設計書の技術選定を踏襲 |
| スタイリング | Tailwind CSS + shadcn/ui | フォーム・テーブル・Cardをゼロから作らず、ポートフォリオ用途で見た目も整えやすい |
| データ取得・更新 | Server Components（GET）+ Server Actions（POST/PATCH/DELETE） | Cookieをサーバーサイドでそのまま扱え、クライアントにfetchロジックを書かずに済む |
| 認証トークン保持 | httpOnly Cookie（Next.js側でGo APIのJWTを中継） | XSS耐性が高く、Go API側にCORS設定が不要になる |
| テスト | Vitest + React Testing Library | Next.js App Routerとの相性が良く、Jestより高速 |

## 3. アーキテクチャ

Next.js App Router を BFF（Backend for Frontend）として使い、ブラウザは常にNext.js自身のオリジンとだけ通信する。Go APIへのリクエストはNext.jsのサーバーサイド（Server Component / Server Action）からのみ発生するため、**Go API側にCORS設定は不要**。

```
ブラウザ
  ↕（同一オリジン、Cookie自動送付）
Next.js (App Router)
  ├─ Server Component（GET系: watchlist一覧取得）
  └─ Server Action（POST/PATCH/DELETE系: 登録・ログイン・追加・削除・閾値変更）
                                          ↓ サーバー間fetch（Authorization: Bearer <jwt>）
                                    Go API（ローカル: http://localhost:8080）
```

- ログイン/登録成功時、Server ActionがGo APIから受け取ったJWTを
  `cookies().set('token', jwt, {httpOnly: true, secure, sameSite: 'lax', maxAge: 60*60*24})` でCookieに格納する（maxAgeはJWTの有効期限24hと一致させる）
- 以降のリクエストはServer Component/ActionがCookieからトークンを読み、`Authorization: Bearer` ヘッダーに詰めてGo APIを呼ぶ
- Go APIのURLは環境変数 `GO_API_URL`（ローカル: `http://localhost:8080`）で切り替える

## 4. 画面構成

| パス | 内容 |
|---|---|
| `/login` | ログインフォーム（email/password）。成功時 `/watchlist` へリダイレクト |
| `/register` | 登録フォーム（email/password）。成功時ログイン扱いで `/watchlist` へリダイレクト |
| `/watchlist` | ログイン必須。保有銘柄一覧（証券コード・閾値・削除ボタン）＋ 銘柄追加フォーム（証券コード・閾値入力） |

- 未ログインで `/watchlist` にアクセスした場合、`middleware.ts` がCookie未存在を検知し `/login` にリダイレクト
- ログイン済みで `/login` `/register` にアクセスした場合は `/watchlist` にリダイレクト
- ログアウトボタン→Server ActionでCookie削除→`/login` へリダイレクト

## 5. エラーハンドリング・バリデーション

- Server Actionは `useActionState` パターンで実装し、Go APIのエラーレスポンス（メール重複・認証失敗・閾値不正など）をフォームのエラーメッセージとしてそのまま表示する
- Go APIから401（トークン期限切れ・無効）が返った場合、該当箇所でCookieを削除し `/login` にリダイレクトする
- クライアント側バリデーション（空欄・型など）は最小限にとどめ、正としてのバリデーションはGo API側に委ねる（二重実装を避ける）

## 6. テスト方針

- Vitest + React Testing Libraryで、Server Actionのロジック（Go APIへのfetchをモック）とフォームコンポーネントの単体テストを書く
- E2E（Playwright等）は今回スコープ外。フォローアップとしてmemoryに記録済み

## 7. ディレクトリ構成（案）

```
frontend/
├── app/
│   ├── login/
│   │   ├── page.tsx
│   │   └── actions.ts        # loginAction
│   ├── register/
│   │   ├── page.tsx
│   │   └── actions.ts        # registerAction
│   ├── watchlist/
│   │   ├── page.tsx           # Server Component: 一覧取得
│   │   └── actions.ts         # addAction / removeAction / updateThresholdAction
│   ├── layout.tsx
│   └── middleware.ts          # 未ログイン時のリダイレクト
├── components/
│   ├── ui/                    # shadcn/ui生成コンポーネント
│   ├── watchlist-table.tsx
│   └── watchlist-form.tsx
├── lib/
│   ├── go-api-client.ts       # Go APIへのfetchラッパー（Authorizationヘッダー付与）
│   └── session.ts             # Cookie読み書きヘルパー
└── package.json
```
