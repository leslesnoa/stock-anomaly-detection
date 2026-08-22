# Phase 4 フロントエンド（Next.js UI）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `frontend/` に Next.js（App Router）アプリを新設し、ユーザー登録・ログイン・watchlist管理（一覧・追加・削除・閾値変更）をブラウザから操作できるようにする。

**Architecture:** Next.js App Router を BFF として使う。ブラウザは Next.js のサーバーサイド（Server Component / Server Action）としか通信せず、Go API へのリクエストは常に Next.js のサーバー側から `Authorization: Bearer <jwt>` を付けて発行する。JWT は Next.js が httpOnly Cookie で保持し、ブラウザの JS からは触れない。Go API が `401`（トークン期限切れ・無効）を返した場合は Cookie を削除して `/login` にリダイレクトする。

**Tech Stack:** Next.js（App Router）+ TypeScript、Tailwind CSS + shadcn/ui、Vitest + React Testing Library

**Spec:** `docs/superpowers/specs/2026-08-19-phase4-frontend-nextjs-ui-design.md`

## Global Constraints

- ブラウザから Go API を直接叩かない。Go API 呼び出しは Server Component / Server Action からのみ行う（Go API 側に CORS 設定を追加しない）
- JWT は Cookie名 `token`、`httpOnly: true`、`sameSite: "lax"`、`secure: NODE_ENV==="production"`、`maxAge: 60*60*24`（Go 側 JWT 有効期限 24h と一致）で保持する
- Go API のベース URL は環境変数 `GO_API_URL`（未設定時のデフォルト `http://localhost:8080`）
- Go API から `401` が返った場合、watchlist 系の Server Component / Server Action は Cookie を削除して `/login` にリダイレクトする（登録・ログイン画面での401はただの認証失敗としてエラーメッセージ表示のみで良い。まだ Cookie を発行していないため削除対象がない）
- クライアント側バリデーションは空欄チェック程度に留め、正のバリデーションは Go API のエラーレスポンスをそのまま表示する
- スコープ外: 通知履歴閲覧、Vercelへの実デプロイ、E2Eテスト、プロフィール編集・パスワード変更
- Go API のレスポンス形式（実装済み・変更しない）:
  - `POST /auth/register` → 成功 `201`（bodyなし）、失敗 `409/400/500` `{"error": string}`
  - `POST /auth/login` → 成功 `200 {"token": string}`、失敗 `401/500` `{"error": string}`
  - `GET /watchlist`（要 `Authorization: Bearer`） → `200 [{"id": string, "stock_code": string, "alert_threshold": number}]`、失敗 `401/500` `{"error": string}`
  - `POST /watchlist`（要 `Authorization: Bearer`、body `{"stock_code": string, "alert_threshold": number}`） → 成功 `201` に上記アイテム、失敗 `401/409/400/500` `{"error": string}`
  - `DELETE /watchlist/{id}`（要 `Authorization: Bearer`） → 成功 `204`、失敗 `401/404/500` `{"error": string}`
  - `PATCH /watchlist/{id}`（要 `Authorization: Bearer`、body `{"alert_threshold": number}`） → 成功 `200`（bodyなし）、失敗 `401/404/400/500` `{"error": string}`

---

## Task 1: Next.js プロジェクト作成 + Vitest セットアップ

**Files:**
- Create: `frontend/`（`create-next-app` が生成する一式）
- Create: `frontend/vitest.config.mts`
- Create: `frontend/vitest.setup.ts`
- Modify: `frontend/package.json`（`test` スクリプト追加）

**Interfaces:**
- Produces: `frontend/` 配下で `npm test` が動く状態。以降のタスクはすべてこの `frontend/` ディレクトリ内で作業する

- [ ] **Step 1: Next.js プロジェクトを作成する**

リポジトリルートで実行:

```bash
npx create-next-app@latest frontend --typescript --tailwind --eslint --app --no-src-dir --import-alias "@/*" --use-npm
```

追加のプロンプト（Turbopack利用の可否など）が出た場合はデフォルト値（Enter）で進める。

- [ ] **Step 2: 開発サーバーが起動することを確認する**

```bash
cd frontend && npm run build
```

Expected: `Compiled successfully` で終了する

- [ ] **Step 3: Vitest 関連パッケージをインストールする**

```bash
cd frontend
npm install -D vitest @vitejs/plugin-react vite-tsconfig-paths jsdom @testing-library/react @testing-library/dom @testing-library/jest-dom @testing-library/user-event
```

- [ ] **Step 4: Vitest 設定ファイルを作成する**

`frontend/vitest.config.mts`:

```typescript
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tsconfigPaths from "vite-tsconfig-paths";

export default defineConfig({
  plugins: [tsconfigPaths(), react()],
  test: {
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
  },
});
```

`frontend/vitest.setup.ts`:

```typescript
import "@testing-library/jest-dom/vitest";
```

- [ ] **Step 5: package.json に test スクリプトを追加する**

`frontend/package.json` の `scripts` に追加:

```json
"test": "vitest run"
```

- [ ] **Step 6: サンプルテストで動作確認する**

`frontend/lib/sample.test.ts`（動作確認用、確認後に削除する）:

```typescript
import { describe, it, expect } from "vitest";

describe("sample", () => {
  it("adds numbers", () => {
    expect(1 + 1).toBe(2);
  });
});
```

Run: `npm test`
Expected: PASS

確認できたら `frontend/lib/sample.test.ts` を削除する。

- [ ] **Step 7: Commit**

```bash
git add frontend
git commit -m "feat: scaffold Next.js frontend project with Vitest"
```

---

## Task 2: shadcn/ui 導入 + 基本コンポーネント追加

**Files:**
- Create: `frontend/components.json`（shadcn設定、CLIが生成）
- Create: `frontend/components/ui/button.tsx`
- Create: `frontend/components/ui/input.tsx`
- Create: `frontend/components/ui/label.tsx`
- Create: `frontend/components/ui/card.tsx`
- Create: `frontend/components/ui/table.tsx`

**Interfaces:**
- Produces: `@/components/ui/{button,input,label,card,table}` から `Button`, `Input`, `Label`, `Card`(+`CardHeader`/`CardContent`/`CardTitle`), `Table`(+`TableHeader`/`TableBody`/`TableRow`/`TableHead`/`TableCell`) をimport可能にする

- [ ] **Step 1: shadcn/ui を初期化する**

```bash
cd frontend
npx shadcn@latest init -d
```

- [ ] **Step 2: 必要なコンポーネントを追加する**

```bash
npx shadcn@latest add button input label card table
```

- [ ] **Step 3: ビルドが通ることを確認する**

Run: `npm run build`
Expected: `Compiled successfully`

- [ ] **Step 4: Commit**

```bash
git add frontend
git commit -m "feat: add shadcn/ui components"
```

---

## Task 3: Go API クライアントラッパー（`lib/go-api-client.ts`）

**Files:**
- Create: `frontend/lib/go-api-client.ts`
- Test: `frontend/lib/go-api-client.test.ts`

**Interfaces:**
- Produces:
  - `type WatchlistItem = { id: string; stock_code: string; alert_threshold: number }`
  - `type ApiFailure = { ok: false; error: string; status: number }`（すべてのAPI関数が失敗時にこの形を返す。呼び出し元は `status === 401` を見てCookie削除+リダイレクトを判断する）
  - `registerUser(email: string, password: string): Promise<{ ok: true } | ApiFailure>`
  - `loginUser(email: string, password: string): Promise<{ ok: true; token: string } | ApiFailure>`
  - `fetchWatchlist(token: string): Promise<{ ok: true; items: WatchlistItem[] } | ApiFailure>`
  - `addWatchlistItem(token: string, stockCode: string, alertThreshold: number): Promise<{ ok: true; item: WatchlistItem } | ApiFailure>`
  - `removeWatchlistItem(token: string, id: string): Promise<{ ok: true } | ApiFailure>`
  - `updateWatchlistThreshold(token: string, id: string, alertThreshold: number): Promise<{ ok: true } | ApiFailure>`

- [ ] **Step 1: 失敗するテストを書く**

`frontend/lib/go-api-client.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  registerUser,
  loginUser,
  fetchWatchlist,
  addWatchlistItem,
  removeWatchlistItem,
  updateWatchlistThreshold,
} from "./go-api-client";

describe("go-api-client", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("registerUser returns ok on 201", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 201 }));

    const result = await registerUser("test@example.com", "password123");

    expect(result).toEqual({ ok: true });
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/auth/register",
      expect.objectContaining({ method: "POST" })
    );
  });

  it("registerUser returns error message and status on 409", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "email already exists" }), { status: 409 })
    );

    const result = await registerUser("test@example.com", "password123");

    expect(result).toEqual({ ok: false, error: "email already exists", status: 409 });
  });

  it("loginUser returns token on 200", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ token: "jwt-token" }), { status: 200 })
    );

    const result = await loginUser("test@example.com", "password123");

    expect(result).toEqual({ ok: true, token: "jwt-token" });
  });

  it("loginUser returns error message and status on 401", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "invalid credentials" }), { status: 401 })
    );

    const result = await loginUser("test@example.com", "wrong-password");

    expect(result).toEqual({ ok: false, error: "invalid credentials", status: 401 });
  });

  it("fetchWatchlist returns items and sends bearer token", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify([{ id: "1", stock_code: "7203", alert_threshold: 2.5 }]),
        { status: 200 }
      )
    );

    const result = await fetchWatchlist("jwt-token");

    expect(result).toEqual({
      ok: true,
      items: [{ id: "1", stock_code: "7203", alert_threshold: 2.5 }],
    });
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/watchlist",
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: "Bearer jwt-token" }),
      })
    );
  });

  it("fetchWatchlist returns status 401 when the token is invalid", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "missing user context" }), { status: 401 })
    );

    const result = await fetchWatchlist("expired-token");

    expect(result).toEqual({ ok: false, error: "missing user context", status: 401 });
  });

  it("addWatchlistItem returns created item on 201", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ id: "1", stock_code: "7203", alert_threshold: 2.5 }), {
        status: 201,
      })
    );

    const result = await addWatchlistItem("jwt-token", "7203", 2.5);

    expect(result).toEqual({
      ok: true,
      item: { id: "1", stock_code: "7203", alert_threshold: 2.5 },
    });
  });

  it("addWatchlistItem returns error message and status on 409", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "stock already in watchlist" }), { status: 409 })
    );

    const result = await addWatchlistItem("jwt-token", "7203", 2.5);

    expect(result).toEqual({ ok: false, error: "stock already in watchlist", status: 409 });
  });

  it("removeWatchlistItem returns ok on 204", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));

    const result = await removeWatchlistItem("jwt-token", "1");

    expect(result).toEqual({ ok: true });
  });

  it("removeWatchlistItem returns error message and status on 404", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "watchlist item not found" }), { status: 404 })
    );

    const result = await removeWatchlistItem("jwt-token", "1");

    expect(result).toEqual({ ok: false, error: "watchlist item not found", status: 404 });
  });

  it("updateWatchlistThreshold returns ok on 200", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));

    const result = await updateWatchlistThreshold("jwt-token", "1", 3.0);

    expect(result).toEqual({ ok: true });
  });

  it("updateWatchlistThreshold returns error message and status on 400", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "invalid threshold" }), { status: 400 })
    );

    const result = await updateWatchlistThreshold("jwt-token", "1", -1);

    expect(result).toEqual({ ok: false, error: "invalid threshold", status: 400 });
  });
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `npm test -- go-api-client`
Expected: FAIL（`go-api-client.ts` が存在しない）

- [ ] **Step 3: 実装する**

`frontend/lib/go-api-client.ts`:

```typescript
export type WatchlistItem = {
  id: string;
  stock_code: string;
  alert_threshold: number;
};

export type ApiFailure = {
  ok: false;
  error: string;
  status: number;
};

type ApiErrorBody = { error: string };

const GO_API_URL = process.env.GO_API_URL ?? "http://localhost:8080";

async function toFailure(res: Response): Promise<ApiFailure> {
  const body = (await res.json()) as ApiErrorBody;
  return { ok: false, error: body.error, status: res.status };
}

export async function registerUser(
  email: string,
  password: string
): Promise<{ ok: true } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/auth/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (res.status === 201) {
    return { ok: true };
  }
  return toFailure(res);
}

export async function loginUser(
  email: string,
  password: string
): Promise<{ ok: true; token: string } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (res.status === 200) {
    const body = (await res.json()) as { token: string };
    return { ok: true, token: body.token };
  }
  return toFailure(res);
}

export async function fetchWatchlist(
  token: string
): Promise<{ ok: true; items: WatchlistItem[] } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/watchlist`, {
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (res.status === 200) {
    const items = (await res.json()) as WatchlistItem[];
    return { ok: true, items };
  }
  return toFailure(res);
}

export async function addWatchlistItem(
  token: string,
  stockCode: string,
  alertThreshold: number
): Promise<{ ok: true; item: WatchlistItem } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/watchlist`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ stock_code: stockCode, alert_threshold: alertThreshold }),
  });
  if (res.status === 201) {
    const item = (await res.json()) as WatchlistItem;
    return { ok: true, item };
  }
  return toFailure(res);
}

export async function removeWatchlistItem(
  token: string,
  id: string
): Promise<{ ok: true } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/watchlist/${id}`, {
    method: "DELETE",
    headers: { Authorization: `Bearer ${token}` },
  });
  if (res.status === 204) {
    return { ok: true };
  }
  return toFailure(res);
}

export async function updateWatchlistThreshold(
  token: string,
  id: string,
  alertThreshold: number
): Promise<{ ok: true } | ApiFailure> {
  const res = await fetch(`${GO_API_URL}/watchlist/${id}`, {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ alert_threshold: alertThreshold }),
  });
  if (res.status === 200) {
    return { ok: true };
  }
  return toFailure(res);
}
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `npm test -- go-api-client`
Expected: PASS（全12ケース）

- [ ] **Step 5: Commit**

```bash
git add frontend/lib/go-api-client.ts frontend/lib/go-api-client.test.ts
git commit -m "feat: add Go API client wrapper"
```

---

## Task 4: セッション管理（`lib/session.ts`）

**Files:**
- Create: `frontend/lib/session.ts`
- Test: `frontend/lib/session.test.ts`

**Interfaces:**
- Consumes: なし
- Produces:
  - `setSessionToken(token: string): Promise<void>`
  - `getSessionToken(): Promise<string | undefined>`
  - `clearSessionToken(): Promise<void>`

- [ ] **Step 1: 失敗するテストを書く**

`frontend/lib/session.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockCookieStore = {
  set: vi.fn(),
  get: vi.fn(),
  delete: vi.fn(),
};

vi.mock("next/headers", () => ({
  cookies: vi.fn(async () => mockCookieStore),
}));

import { setSessionToken, getSessionToken, clearSessionToken } from "./session";

describe("session", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("setSessionToken sets an httpOnly cookie named token", async () => {
    await setSessionToken("jwt-token");

    expect(mockCookieStore.set).toHaveBeenCalledWith(
      "token",
      "jwt-token",
      expect.objectContaining({
        httpOnly: true,
        sameSite: "lax",
        maxAge: 60 * 60 * 24,
        path: "/",
      })
    );
  });

  it("getSessionToken returns the cookie value", async () => {
    mockCookieStore.get.mockReturnValue({ value: "jwt-token" });

    const token = await getSessionToken();

    expect(token).toBe("jwt-token");
    expect(mockCookieStore.get).toHaveBeenCalledWith("token");
  });

  it("getSessionToken returns undefined when cookie is absent", async () => {
    mockCookieStore.get.mockReturnValue(undefined);

    const token = await getSessionToken();

    expect(token).toBeUndefined();
  });

  it("clearSessionToken deletes the cookie", async () => {
    await clearSessionToken();

    expect(mockCookieStore.delete).toHaveBeenCalledWith("token");
  });
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `npm test -- session`
Expected: FAIL（`session.ts` が存在しない）

- [ ] **Step 3: 実装する**

`frontend/lib/session.ts`:

```typescript
import { cookies } from "next/headers";

const COOKIE_NAME = "token";
const MAX_AGE_SECONDS = 60 * 60 * 24;

export async function setSessionToken(token: string): Promise<void> {
  const cookieStore = await cookies();
  cookieStore.set(COOKIE_NAME, token, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    maxAge: MAX_AGE_SECONDS,
    path: "/",
  });
}

export async function getSessionToken(): Promise<string | undefined> {
  const cookieStore = await cookies();
  return cookieStore.get(COOKIE_NAME)?.value;
}

export async function clearSessionToken(): Promise<void> {
  const cookieStore = await cookies();
  cookieStore.delete(COOKIE_NAME);
}
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `npm test -- session`
Expected: PASS（全4ケース）

- [ ] **Step 5: Commit**

```bash
git add frontend/lib/session.ts frontend/lib/session.test.ts
git commit -m "feat: add session cookie helpers"
```

---

## Task 5: 登録画面（`/register`）

**Files:**
- Create: `frontend/app/register/actions.ts`
- Create: `frontend/app/register/page.tsx`
- Create: `frontend/components/register-form.tsx`
- Test: `frontend/app/register/actions.test.ts`
- Test: `frontend/components/register-form.test.tsx`

**Interfaces:**
- Consumes: `registerUser`, `loginUser`（Task3）、`setSessionToken`（Task4）
- Produces:
  - `type AuthFormState = { error?: string }`
  - `registerAction(prevState: AuthFormState, formData: FormData): Promise<AuthFormState>`
  - `<RegisterForm />` コンポーネント

- [ ] **Step 1: actions.ts の失敗するテストを書く**

`frontend/app/register/actions.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/go-api-client", () => ({
  registerUser: vi.fn(),
  loginUser: vi.fn(),
}));
vi.mock("@/lib/session", () => ({
  setSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn(),
}));

import { registerAction } from "./actions";
import * as goApiClient from "@/lib/go-api-client";
import * as session from "@/lib/session";
import { redirect } from "next/navigation";

function formData(email: string, password: string): FormData {
  const fd = new FormData();
  fd.set("email", email);
  fd.set("password", password);
  return fd;
}

describe("registerAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns an error when registration fails", async () => {
    vi.mocked(goApiClient.registerUser).mockResolvedValue({
      ok: false,
      error: "email already exists",
      status: 409,
    });

    const result = await registerAction({}, formData("test@example.com", "password123"));

    expect(result).toEqual({ error: "email already exists" });
    expect(goApiClient.loginUser).not.toHaveBeenCalled();
  });

  it("logs in and redirects to /watchlist on successful registration", async () => {
    vi.mocked(goApiClient.registerUser).mockResolvedValue({ ok: true });
    vi.mocked(goApiClient.loginUser).mockResolvedValue({ ok: true, token: "jwt-token" });

    await registerAction({}, formData("test@example.com", "password123"));

    expect(session.setSessionToken).toHaveBeenCalledWith("jwt-token");
    expect(redirect).toHaveBeenCalledWith("/watchlist");
  });

  it("returns an error when the post-registration login fails", async () => {
    vi.mocked(goApiClient.registerUser).mockResolvedValue({ ok: true });
    vi.mocked(goApiClient.loginUser).mockResolvedValue({
      ok: false,
      error: "invalid credentials",
      status: 401,
    });

    const result = await registerAction({}, formData("test@example.com", "password123"));

    expect(result).toEqual({ error: "invalid credentials" });
    expect(redirect).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `npm test -- app/register/actions`
Expected: FAIL（`actions.ts` が存在しない）

- [ ] **Step 3: actions.ts を実装する**

`frontend/app/register/actions.ts`:

```typescript
"use server";

import { redirect } from "next/navigation";
import { registerUser, loginUser } from "@/lib/go-api-client";
import { setSessionToken } from "@/lib/session";

export type AuthFormState = {
  error?: string;
};

export async function registerAction(
  _prevState: AuthFormState,
  formData: FormData
): Promise<AuthFormState> {
  const email = formData.get("email");
  const password = formData.get("password");
  if (typeof email !== "string" || typeof password !== "string") {
    return { error: "invalid form data" };
  }

  const registerResult = await registerUser(email, password);
  if (!registerResult.ok) {
    return { error: registerResult.error };
  }

  const loginResult = await loginUser(email, password);
  if (!loginResult.ok) {
    return { error: loginResult.error };
  }

  await setSessionToken(loginResult.token);
  redirect("/watchlist");
}
```

登録・ログイン画面ではまだ Cookie を発行していないため、ここでの401（＝ログイン失敗）はエラーメッセージ表示のみで良い（Cookie削除・リダイレクトの対象は「発行済みCookieが無効になった」ケースのみ）。

- [ ] **Step 4: actions.ts のテストが通ることを確認する**

Run: `npm test -- app/register/actions`
Expected: PASS（全3ケース）

- [ ] **Step 5: RegisterForm の失敗するテストを書く**

`frontend/components/register-form.test.tsx`:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

vi.mock("@/app/register/actions", () => ({
  registerAction: vi.fn(async () => ({ error: "email already exists" })),
}));

import { RegisterForm } from "./register-form";

describe("RegisterForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders email and password fields", () => {
    render(<RegisterForm />);

    expect(screen.getByLabelText("メールアドレス")).toBeInTheDocument();
    expect(screen.getByLabelText("パスワード")).toBeInTheDocument();
  });

  it("shows the error message returned by registerAction", async () => {
    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("メールアドレス"), {
      target: { value: "test@example.com" },
    });
    fireEvent.change(screen.getByLabelText("パスワード"), {
      target: { value: "password123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "登録" }));

    await waitFor(() => {
      expect(screen.getByText("email already exists")).toBeInTheDocument();
    });
  });
});
```

- [ ] **Step 6: テストが失敗することを確認する**

Run: `npm test -- register-form`
Expected: FAIL（`register-form.tsx` が存在しない）

- [ ] **Step 7: RegisterForm を実装する**

`frontend/components/register-form.tsx`:

```typescript
"use client";

import { useActionState } from "react";
import { registerAction, type AuthFormState } from "@/app/register/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const initialState: AuthFormState = {};

export function RegisterForm() {
  const [state, formAction, isPending] = useActionState(registerAction, initialState);

  return (
    <form action={formAction} className="flex flex-col gap-4">
      <div>
        <Label htmlFor="email">メールアドレス</Label>
        <Input id="email" name="email" type="email" required />
      </div>
      <div>
        <Label htmlFor="password">パスワード</Label>
        <Input id="password" name="password" type="password" required minLength={8} />
      </div>
      <Button type="submit" disabled={isPending}>
        登録
      </Button>
      {state.error && <p className="text-sm text-red-500">{state.error}</p>}
    </form>
  );
}
```

- [ ] **Step 8: RegisterForm のテストが通ることを確認する**

Run: `npm test -- register-form`
Expected: PASS（全2ケース）

- [ ] **Step 9: page.tsx を実装する**

`frontend/app/register/page.tsx`:

```typescript
import { RegisterForm } from "@/components/register-form";

export default function RegisterPage() {
  return (
    <main className="mx-auto flex max-w-sm flex-col gap-6 p-8">
      <h1 className="text-2xl font-bold">新規登録</h1>
      <RegisterForm />
      <a href="/login" className="text-sm underline">
        既にアカウントをお持ちの方はこちら
      </a>
    </main>
  );
}
```

- [ ] **Step 10: ビルドが通ることを確認する**

Run: `npm run build`
Expected: `Compiled successfully`

- [ ] **Step 11: Commit**

```bash
git add frontend/app/register frontend/components/register-form.tsx frontend/components/register-form.test.tsx
git commit -m "feat: add registration page"
```

---

## Task 6: ログイン画面（`/login`）

**Files:**
- Create: `frontend/app/login/actions.ts`
- Create: `frontend/app/login/page.tsx`
- Create: `frontend/components/login-form.tsx`
- Test: `frontend/app/login/actions.test.ts`
- Test: `frontend/components/login-form.test.tsx`

**Interfaces:**
- Consumes: `loginUser`（Task3）、`setSessionToken`（Task4）
- Produces: `type AuthFormState = { error?: string }`、`loginAction(prevState: AuthFormState, formData: FormData): Promise<AuthFormState>`、`<LoginForm />`

- [ ] **Step 1: actions.ts の失敗するテストを書く**

`frontend/app/login/actions.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/go-api-client", () => ({
  loginUser: vi.fn(),
}));
vi.mock("@/lib/session", () => ({
  setSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn(),
}));

import { loginAction } from "./actions";
import * as goApiClient from "@/lib/go-api-client";
import * as session from "@/lib/session";
import { redirect } from "next/navigation";

function formData(email: string, password: string): FormData {
  const fd = new FormData();
  fd.set("email", email);
  fd.set("password", password);
  return fd;
}

describe("loginAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns an error when login fails", async () => {
    vi.mocked(goApiClient.loginUser).mockResolvedValue({
      ok: false,
      error: "invalid credentials",
      status: 401,
    });

    const result = await loginAction({}, formData("test@example.com", "wrong-password"));

    expect(result).toEqual({ error: "invalid credentials" });
  });

  it("sets the session cookie and redirects to /watchlist on success", async () => {
    vi.mocked(goApiClient.loginUser).mockResolvedValue({ ok: true, token: "jwt-token" });

    await loginAction({}, formData("test@example.com", "password123"));

    expect(session.setSessionToken).toHaveBeenCalledWith("jwt-token");
    expect(redirect).toHaveBeenCalledWith("/watchlist");
  });
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `npm test -- app/login/actions`
Expected: FAIL（`actions.ts` が存在しない）

- [ ] **Step 3: actions.ts を実装する**

`frontend/app/login/actions.ts`:

```typescript
"use server";

import { redirect } from "next/navigation";
import { loginUser } from "@/lib/go-api-client";
import { setSessionToken } from "@/lib/session";

export type AuthFormState = {
  error?: string;
};

export async function loginAction(
  _prevState: AuthFormState,
  formData: FormData
): Promise<AuthFormState> {
  const email = formData.get("email");
  const password = formData.get("password");
  if (typeof email !== "string" || typeof password !== "string") {
    return { error: "invalid form data" };
  }

  const result = await loginUser(email, password);
  if (!result.ok) {
    return { error: result.error };
  }

  await setSessionToken(result.token);
  redirect("/watchlist");
}
```

- [ ] **Step 4: actions.ts のテストが通ることを確認する**

Run: `npm test -- app/login/actions`
Expected: PASS（全2ケース）

- [ ] **Step 5: LoginForm の失敗するテストを書く**

`frontend/components/login-form.test.tsx`:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

vi.mock("@/app/login/actions", () => ({
  loginAction: vi.fn(async () => ({ error: "invalid credentials" })),
}));

import { LoginForm } from "./login-form";

describe("LoginForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders email and password fields", () => {
    render(<LoginForm />);

    expect(screen.getByLabelText("メールアドレス")).toBeInTheDocument();
    expect(screen.getByLabelText("パスワード")).toBeInTheDocument();
  });

  it("shows the error message returned by loginAction", async () => {
    render(<LoginForm />);

    fireEvent.change(screen.getByLabelText("メールアドレス"), {
      target: { value: "test@example.com" },
    });
    fireEvent.change(screen.getByLabelText("パスワード"), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "ログイン" }));

    await waitFor(() => {
      expect(screen.getByText("invalid credentials")).toBeInTheDocument();
    });
  });
});
```

- [ ] **Step 6: テストが失敗することを確認する**

Run: `npm test -- login-form`
Expected: FAIL（`login-form.tsx` が存在しない）

- [ ] **Step 7: LoginForm を実装する**

`frontend/components/login-form.tsx`:

```typescript
"use client";

import { useActionState } from "react";
import { loginAction, type AuthFormState } from "@/app/login/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const initialState: AuthFormState = {};

export function LoginForm() {
  const [state, formAction, isPending] = useActionState(loginAction, initialState);

  return (
    <form action={formAction} className="flex flex-col gap-4">
      <div>
        <Label htmlFor="email">メールアドレス</Label>
        <Input id="email" name="email" type="email" required />
      </div>
      <div>
        <Label htmlFor="password">パスワード</Label>
        <Input id="password" name="password" type="password" required />
      </div>
      <Button type="submit" disabled={isPending}>
        ログイン
      </Button>
      {state.error && <p className="text-sm text-red-500">{state.error}</p>}
    </form>
  );
}
```

- [ ] **Step 8: LoginForm のテストが通ることを確認する**

Run: `npm test -- login-form`
Expected: PASS（全2ケース）

- [ ] **Step 9: page.tsx を実装する**

`frontend/app/login/page.tsx`:

```typescript
import { LoginForm } from "@/components/login-form";

export default function LoginPage() {
  return (
    <main className="mx-auto flex max-w-sm flex-col gap-6 p-8">
      <h1 className="text-2xl font-bold">ログイン</h1>
      <LoginForm />
      <a href="/register" className="text-sm underline">
        アカウントをお持ちでない方はこちら
      </a>
    </main>
  );
}
```

- [ ] **Step 10: ビルドが通ることを確認する**

Run: `npm run build`
Expected: `Compiled successfully`

- [ ] **Step 11: Commit**

```bash
git add frontend/app/login frontend/components/login-form.tsx frontend/components/login-form.test.tsx
git commit -m "feat: add login page"
```

---

## Task 7: 認証ミドルウェア（`middleware.ts`）

**Files:**
- Create: `frontend/middleware.ts`
- Test: `frontend/middleware.test.ts`

**Interfaces:**
- Consumes: なし（Cookie名 `"token"` は Task4 の `session.ts` と一致させる）
- Produces: `middleware(request: NextRequest): NextResponse`、`config.matcher`

- [ ] **Step 1: 失敗するテストを書く**

`frontend/middleware.test.ts`:

```typescript
import { describe, it, expect } from "vitest";
import { NextRequest } from "next/server";
import { middleware } from "./middleware";

describe("middleware", () => {
  it("redirects to /login when accessing /watchlist without a token cookie", () => {
    const req = new NextRequest("http://localhost:3000/watchlist");

    const res = middleware(req);

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("http://localhost:3000/login");
  });

  it("passes through to /watchlist when a token cookie is present", () => {
    const req = new NextRequest("http://localhost:3000/watchlist", {
      headers: { cookie: "token=jwt-token" },
    });

    const res = middleware(req);

    expect(res.status).toBe(200);
  });

  it("redirects to /watchlist when accessing /login with a token cookie", () => {
    const req = new NextRequest("http://localhost:3000/login", {
      headers: { cookie: "token=jwt-token" },
    });

    const res = middleware(req);

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("http://localhost:3000/watchlist");
  });

  it("passes through to /login when no token cookie is present", () => {
    const req = new NextRequest("http://localhost:3000/login");

    const res = middleware(req);

    expect(res.status).toBe(200);
  });
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `npm test -- middleware`
Expected: FAIL（`middleware.ts` が存在しない）

- [ ] **Step 3: 実装する**

`frontend/middleware.ts`:

```typescript
import { NextRequest, NextResponse } from "next/server";

const PROTECTED_PATHS = ["/watchlist"];
const AUTH_PATHS = ["/login", "/register"];

export function middleware(request: NextRequest) {
  const token = request.cookies.get("token")?.value;
  const { pathname } = request.nextUrl;

  if (PROTECTED_PATHS.some((p) => pathname.startsWith(p)) && !token) {
    return NextResponse.redirect(new URL("/login", request.url));
  }

  if (AUTH_PATHS.some((p) => pathname.startsWith(p)) && token) {
    return NextResponse.redirect(new URL("/watchlist", request.url));
  }

  return NextResponse.next();
}

export const config = {
  matcher: ["/watchlist/:path*", "/login", "/register"],
};
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `npm test -- middleware`
Expected: PASS（全4ケース）

- [ ] **Step 5: Commit**

```bash
git add frontend/middleware.ts frontend/middleware.test.ts
git commit -m "feat: add auth redirect middleware"
```

---

## Task 8: watchlist 一覧ページ

**Files:**
- Create: `frontend/app/watchlist/actions.ts`
- Create: `frontend/app/watchlist/page.tsx`
- Create: `frontend/components/watchlist-table.tsx`
- Test: `frontend/app/watchlist/actions.test.ts`
- Test: `frontend/components/watchlist-table.test.tsx`

**Interfaces:**
- Consumes: `fetchWatchlist`, `WatchlistItem`, `ApiFailure`（Task3）、`getSessionToken`, `clearSessionToken`（Task4）
- Produces:
  - `type ActionResult = { ok: true } | { ok: false; error: string }`
  - `requireToken(): Promise<{ ok: true; token: string } | { ok: false; error: string }>`
  - `redirectIfUnauthorized(result: ApiFailure): Promise<never | void>`（`status === 401` のとき Cookie を消して `/login` にリダイレクトする。それ以外は何もしない）
  - `<WatchlistTable items={WatchlistItem[]} />`（Task9・Task10がこのファイルの `actions.ts` に関数を追加する）

- [ ] **Step 1: actions.ts の失敗するテストを書く**

`frontend/app/watchlist/actions.test.ts`（`requireToken`・`redirectIfUnauthorized` の単体テスト）:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/session", () => ({
  getSessionToken: vi.fn(),
  clearSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn(),
}));

import { requireToken, redirectIfUnauthorized } from "./actions";
import * as session from "@/lib/session";
import { redirect } from "next/navigation";

describe("requireToken", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns the token when a session cookie exists", async () => {
    vi.mocked(session.getSessionToken).mockResolvedValue("jwt-token");

    const result = await requireToken();

    expect(result).toEqual({ ok: true, token: "jwt-token" });
  });

  it("returns an error when no session cookie exists", async () => {
    vi.mocked(session.getSessionToken).mockResolvedValue(undefined);

    const result = await requireToken();

    expect(result).toEqual({ ok: false, error: "unauthorized" });
  });
});

describe("redirectIfUnauthorized", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("clears the session and redirects on a 401 failure", async () => {
    await redirectIfUnauthorized({ ok: false, error: "missing user context", status: 401 });

    expect(session.clearSessionToken).toHaveBeenCalled();
    expect(redirect).toHaveBeenCalledWith("/login");
  });

  it("does nothing on a non-401 failure", async () => {
    await redirectIfUnauthorized({ ok: false, error: "internal server error", status: 500 });

    expect(session.clearSessionToken).not.toHaveBeenCalled();
    expect(redirect).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `npm test -- app/watchlist/actions`
Expected: FAIL（`actions.ts` が存在しない）

- [ ] **Step 3: actions.ts の土台を実装する**

`frontend/app/watchlist/actions.ts`:

```typescript
"use server";

import { redirect } from "next/navigation";
import type { ApiFailure } from "@/lib/go-api-client";
import { getSessionToken, clearSessionToken } from "@/lib/session";

export type ActionResult = { ok: true } | { ok: false; error: string };

export async function requireToken(): Promise<
  { ok: true; token: string } | { ok: false; error: string }
> {
  const token = await getSessionToken();
  if (!token) {
    return { ok: false, error: "unauthorized" };
  }
  return { ok: true, token };
}

export async function redirectIfUnauthorized(result: ApiFailure): Promise<void> {
  if (result.status === 401) {
    await clearSessionToken();
    redirect("/login");
  }
}
```

- [ ] **Step 4: actions.ts のテストが通ることを確認する**

Run: `npm test -- app/watchlist/actions`
Expected: PASS（全4ケース）

- [ ] **Step 5: WatchlistTable の失敗するテストを書く**

`frontend/components/watchlist-table.test.tsx`:

```typescript
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { WatchlistTable } from "./watchlist-table";

describe("WatchlistTable", () => {
  it("renders each watchlist item's stock code and threshold", () => {
    render(
      <WatchlistTable
        items={[
          { id: "1", stock_code: "7203", alert_threshold: 2.5 },
          { id: "2", stock_code: "9984", alert_threshold: 3.0 },
        ]}
      />
    );

    expect(screen.getByText("7203")).toBeInTheDocument();
    expect(screen.getByText("9984")).toBeInTheDocument();
  });

  it("renders nothing in the body when items is empty", () => {
    render(<WatchlistTable items={[]} />);

    expect(screen.queryByRole("row", { name: /./ })).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 6: テストが失敗することを確認する**

Run: `npm test -- watchlist-table`
Expected: FAIL（`watchlist-table.tsx` が存在しない）

- [ ] **Step 7: WatchlistTable を実装する（この時点では表示のみ、削除・閾値変更はTask10で追加）**

`frontend/components/watchlist-table.tsx`:

```typescript
import type { WatchlistItem } from "@/lib/go-api-client";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function WatchlistTable({ items }: { items: WatchlistItem[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>証券コード</TableHead>
          <TableHead>閾値</TableHead>
          <TableHead />
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => (
          <TableRow key={item.id}>
            <TableCell>{item.stock_code}</TableCell>
            <TableCell>{item.alert_threshold}</TableCell>
            <TableCell />
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
```

- [ ] **Step 8: テストが通ることを確認する**

Run: `npm test -- watchlist-table`
Expected: PASS（全2ケース）

- [ ] **Step 9: page.tsx を実装する**

`frontend/app/watchlist/page.tsx`:

```typescript
import { redirect } from "next/navigation";
import { fetchWatchlist } from "@/lib/go-api-client";
import { WatchlistTable } from "@/components/watchlist-table";
import { requireToken, redirectIfUnauthorized } from "./actions";

export default async function WatchlistPage() {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    redirect("/login");
  }

  const result = await fetchWatchlist(tokenResult.token);
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    throw new Error(result.error);
  }

  return (
    <main className="mx-auto max-w-2xl p-8">
      <h1 className="mb-4 text-2xl font-bold">保有銘柄</h1>
      <WatchlistTable items={result.items} />
    </main>
  );
}
```

- [ ] **Step 10: ビルドが通ることを確認する**

Run: `npm run build`
Expected: `Compiled successfully`

- [ ] **Step 11: Commit**

```bash
git add frontend/app/watchlist frontend/components/watchlist-table.tsx frontend/components/watchlist-table.test.tsx
git commit -m "feat: add watchlist listing page"
```

---

## Task 9: 銘柄追加フォーム

**Files:**
- Modify: `frontend/app/watchlist/actions.ts`（`addAction` 追加）
- Modify: `frontend/app/watchlist/page.tsx`（`AddWatchlistForm` 追加）
- Create: `frontend/components/add-watchlist-form.tsx`
- Test: `frontend/app/watchlist/actions.test.ts`
- Test: `frontend/components/add-watchlist-form.test.tsx`

**Interfaces:**
- Consumes: `addWatchlistItem`（Task3）、`requireToken`, `redirectIfUnauthorized`（Task8）
- Produces: `addAction(prevState: ActionResult | null, formData: FormData): Promise<ActionResult>`、`<AddWatchlistForm />`

- [ ] **Step 1: addAction の失敗するテストを追記する**

`frontend/app/watchlist/actions.test.ts` に追記:

```typescript
vi.mock("@/lib/go-api-client", () => ({
  addWatchlistItem: vi.fn(),
}));

import { addAction } from "./actions";
import * as goApiClient from "@/lib/go-api-client";

function watchlistFormData(stockCode: string, threshold: string): FormData {
  const fd = new FormData();
  fd.set("stock_code", stockCode);
  fd.set("alert_threshold", threshold);
  return fd;
}

describe("addAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(session.getSessionToken).mockResolvedValue("jwt-token");
  });

  it("returns ok on success", async () => {
    vi.mocked(goApiClient.addWatchlistItem).mockResolvedValue({
      ok: true,
      item: { id: "1", stock_code: "7203", alert_threshold: 2.5 },
    });

    const result = await addAction(null, watchlistFormData("7203", "2.5"));

    expect(result).toEqual({ ok: true });
  });

  it("returns the error message when the API call fails", async () => {
    vi.mocked(goApiClient.addWatchlistItem).mockResolvedValue({
      ok: false,
      error: "stock already in watchlist",
      status: 409,
    });

    const result = await addAction(null, watchlistFormData("7203", "2.5"));

    expect(result).toEqual({ ok: false, error: "stock already in watchlist" });
  });

  it("redirects to /login when the API call returns 401", async () => {
    vi.mocked(goApiClient.addWatchlistItem).mockResolvedValue({
      ok: false,
      error: "missing user context",
      status: 401,
    });

    await addAction(null, watchlistFormData("7203", "2.5"));

    expect(session.clearSessionToken).toHaveBeenCalled();
    expect(redirect).toHaveBeenCalledWith("/login");
  });
});
```

Task8時点の `actions.test.ts` には `vi.mock("@/lib/go-api-client", ...)` が無いため、この Task で新規に追加する。

- [ ] **Step 2: テストが失敗することを確認する**

Run: `npm test -- app/watchlist/actions`
Expected: FAIL（`addAction` が存在しない）

- [ ] **Step 3: `addAction` を追加する**

`frontend/app/watchlist/actions.ts` の import 文を以下に更新し:

```typescript
"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { addWatchlistItem, type ApiFailure } from "@/lib/go-api-client";
import { getSessionToken, clearSessionToken } from "@/lib/session";
```

ファイル末尾に追加:

```typescript
export async function addAction(
  _prevState: ActionResult | null,
  formData: FormData
): Promise<ActionResult> {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    return tokenResult;
  }
  const stockCode = formData.get("stock_code");
  const alertThreshold = formData.get("alert_threshold");
  if (typeof stockCode !== "string" || typeof alertThreshold !== "string") {
    return { ok: false, error: "invalid form data" };
  }
  const result = await addWatchlistItem(tokenResult.token, stockCode, Number(alertThreshold));
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    return { ok: false, error: result.error };
  }
  revalidatePath("/watchlist");
  return { ok: true };
}
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `npm test -- app/watchlist/actions`
Expected: PASS（全7ケース）

- [ ] **Step 5: AddWatchlistForm の失敗するテストを書く**

`frontend/components/add-watchlist-form.test.tsx`:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

vi.mock("@/app/watchlist/actions", () => ({
  addAction: vi.fn(async () => ({ ok: false, error: "stock already in watchlist" })),
}));

import { AddWatchlistForm } from "./add-watchlist-form";

describe("AddWatchlistForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders stock code and threshold fields", () => {
    render(<AddWatchlistForm />);

    expect(screen.getByLabelText("証券コード")).toBeInTheDocument();
    expect(screen.getByLabelText("閾値")).toBeInTheDocument();
  });

  it("shows the error message returned by addAction", async () => {
    render(<AddWatchlistForm />);

    fireEvent.change(screen.getByLabelText("証券コード"), { target: { value: "7203" } });
    fireEvent.click(screen.getByRole("button", { name: "追加" }));

    await waitFor(() => {
      expect(screen.getByText("stock already in watchlist")).toBeInTheDocument();
    });
  });
});
```

- [ ] **Step 6: テストが失敗することを確認する**

Run: `npm test -- add-watchlist-form`
Expected: FAIL（`add-watchlist-form.tsx` が存在しない）

- [ ] **Step 7: AddWatchlistForm を実装する**

`frontend/components/add-watchlist-form.tsx`:

```typescript
"use client";

import { useActionState } from "react";
import { addAction, type ActionResult } from "@/app/watchlist/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const initialState: ActionResult | null = null;

export function AddWatchlistForm() {
  const [state, formAction, isPending] = useActionState(addAction, initialState);

  return (
    <form action={formAction} className="mb-6 flex items-end gap-2">
      <div>
        <Label htmlFor="stock_code">証券コード</Label>
        <Input id="stock_code" name="stock_code" placeholder="7203" required />
      </div>
      <div>
        <Label htmlFor="alert_threshold">閾値</Label>
        <Input
          id="alert_threshold"
          name="alert_threshold"
          type="number"
          step="0.1"
          defaultValue="2.5"
          required
        />
      </div>
      <Button type="submit" disabled={isPending}>
        追加
      </Button>
      {state && !state.ok && <p className="text-sm text-red-500">{state.error}</p>}
    </form>
  );
}
```

- [ ] **Step 8: テストが通ることを確認する**

Run: `npm test -- add-watchlist-form`
Expected: PASS（全2ケース）

- [ ] **Step 9: page.tsx に組み込む**

`frontend/app/watchlist/page.tsx` を編集:

```typescript
import { redirect } from "next/navigation";
import { fetchWatchlist } from "@/lib/go-api-client";
import { WatchlistTable } from "@/components/watchlist-table";
import { AddWatchlistForm } from "@/components/add-watchlist-form";
import { requireToken, redirectIfUnauthorized } from "./actions";

export default async function WatchlistPage() {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    redirect("/login");
  }

  const result = await fetchWatchlist(tokenResult.token);
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    throw new Error(result.error);
  }

  return (
    <main className="mx-auto max-w-2xl p-8">
      <h1 className="mb-4 text-2xl font-bold">保有銘柄</h1>
      <AddWatchlistForm />
      <WatchlistTable items={result.items} />
    </main>
  );
}
```

- [ ] **Step 10: ビルドが通ることを確認する**

Run: `npm run build`
Expected: `Compiled successfully`

- [ ] **Step 11: Commit**

```bash
git add frontend/app/watchlist frontend/components/add-watchlist-form.tsx frontend/components/add-watchlist-form.test.tsx
git commit -m "feat: add watchlist item creation form"
```

---

## Task 10: 銘柄削除・閾値変更

**Files:**
- Modify: `frontend/app/watchlist/actions.ts`（`removeAction`, `updateThresholdAction` 追加）
- Modify: `frontend/components/watchlist-table.tsx`（行ごとの削除・閾値編集UIを追加）
- Test: `frontend/app/watchlist/actions.test.ts`（追記）
- Test: `frontend/components/watchlist-table.test.tsx`（追記）

**Interfaces:**
- Consumes: `removeWatchlistItem`, `updateWatchlistThreshold`（Task3）、`requireToken`, `redirectIfUnauthorized`（Task8）
- Produces: `removeAction(id: string): Promise<ActionResult>`、`updateThresholdAction(id: string, alertThreshold: number): Promise<ActionResult>`

- [ ] **Step 1: 失敗するテストを追記する**

`frontend/app/watchlist/actions.test.ts` の `vi.mock("@/lib/go-api-client", ...)` を以下に更新:

```typescript
vi.mock("@/lib/go-api-client", () => ({
  addWatchlistItem: vi.fn(),
  removeWatchlistItem: vi.fn(),
  updateWatchlistThreshold: vi.fn(),
}));
```

ファイル末尾に追加:

```typescript
import { removeAction, updateThresholdAction } from "./actions";

describe("removeAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(session.getSessionToken).mockResolvedValue("jwt-token");
  });

  it("returns ok and revalidates /watchlist on success", async () => {
    vi.mocked(goApiClient.removeWatchlistItem).mockResolvedValue({ ok: true });

    const result = await removeAction("1");

    expect(result).toEqual({ ok: true });
  });

  it("returns the error message when the API call fails", async () => {
    vi.mocked(goApiClient.removeWatchlistItem).mockResolvedValue({
      ok: false,
      error: "watchlist item not found",
      status: 404,
    });

    const result = await removeAction("1");

    expect(result).toEqual({ ok: false, error: "watchlist item not found" });
  });
});

describe("updateThresholdAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(session.getSessionToken).mockResolvedValue("jwt-token");
  });

  it("returns ok on success", async () => {
    vi.mocked(goApiClient.updateWatchlistThreshold).mockResolvedValue({ ok: true });

    const result = await updateThresholdAction("1", 3.0);

    expect(result).toEqual({ ok: true });
  });

  it("returns the error message when the API call fails", async () => {
    vi.mocked(goApiClient.updateWatchlistThreshold).mockResolvedValue({
      ok: false,
      error: "invalid threshold",
      status: 400,
    });

    const result = await updateThresholdAction("1", -1);

    expect(result).toEqual({ ok: false, error: "invalid threshold" });
  });
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `npm test -- app/watchlist/actions`
Expected: FAIL（`removeAction`, `updateThresholdAction` が存在しない）

- [ ] **Step 3: `removeAction`, `updateThresholdAction` を追加する**

`frontend/app/watchlist/actions.ts` の import 文を以下に更新し:

```typescript
import {
  addWatchlistItem,
  removeWatchlistItem,
  updateWatchlistThreshold,
  type ApiFailure,
} from "@/lib/go-api-client";
```

ファイル末尾に追加:

```typescript
export async function removeAction(id: string): Promise<ActionResult> {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    return tokenResult;
  }
  const result = await removeWatchlistItem(tokenResult.token, id);
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    return { ok: false, error: result.error };
  }
  revalidatePath("/watchlist");
  return { ok: true };
}

export async function updateThresholdAction(
  id: string,
  alertThreshold: number
): Promise<ActionResult> {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    return tokenResult;
  }
  const result = await updateWatchlistThreshold(tokenResult.token, id, alertThreshold);
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    return { ok: false, error: result.error };
  }
  revalidatePath("/watchlist");
  return { ok: true };
}
```

- [ ] **Step 4: actions.ts のテストが通ることを確認する**

Run: `npm test -- app/watchlist/actions`
Expected: PASS（全11ケース）

- [ ] **Step 5: WatchlistTable の失敗するテストを追記する**

`frontend/components/watchlist-table.test.tsx` の冒頭 import を以下に更新:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { WatchlistTable } from "./watchlist-table";

vi.mock("@/app/watchlist/actions", () => ({
  removeAction: vi.fn(),
  updateThresholdAction: vi.fn(),
}));

import * as actions from "@/app/watchlist/actions";
```

ファイル末尾に追加:

```typescript
describe("WatchlistTable row actions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("calls removeAction with the item id when the delete button is clicked", async () => {
    vi.mocked(actions.removeAction).mockResolvedValue({ ok: true });

    render(
      <WatchlistTable items={[{ id: "1", stock_code: "7203", alert_threshold: 2.5 }]} />
    );
    fireEvent.click(screen.getByRole("button", { name: "削除" }));

    await waitFor(() => {
      expect(actions.removeAction).toHaveBeenCalledWith("1");
    });
  });

  it("calls updateThresholdAction with the new value on blur", async () => {
    vi.mocked(actions.updateThresholdAction).mockResolvedValue({ ok: true });

    render(
      <WatchlistTable items={[{ id: "1", stock_code: "7203", alert_threshold: 2.5 }]} />
    );
    const input = screen.getByDisplayValue("2.5");
    fireEvent.change(input, { target: { value: "3" } });
    fireEvent.blur(input);

    await waitFor(() => {
      expect(actions.updateThresholdAction).toHaveBeenCalledWith("1", 3);
    });
  });
});
```

- [ ] **Step 6: テストが失敗することを確認する**

Run: `npm test -- watchlist-table`
Expected: FAIL（削除・閾値変更のUIが未実装）

- [ ] **Step 7: WatchlistTable を拡張する**

`frontend/components/watchlist-table.tsx` を全面差し替え:

```typescript
"use client";

import { useState, useTransition } from "react";
import type { WatchlistItem } from "@/lib/go-api-client";
import { removeAction, updateThresholdAction } from "@/app/watchlist/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function WatchlistTable({ items }: { items: WatchlistItem[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>証券コード</TableHead>
          <TableHead>閾値</TableHead>
          <TableHead />
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => (
          <WatchlistRow key={item.id} item={item} />
        ))}
      </TableBody>
    </Table>
  );
}

function WatchlistRow({ item }: { item: WatchlistItem }) {
  const [threshold, setThreshold] = useState(String(item.alert_threshold));
  const [error, setError] = useState<string | null>(null);
  const [isPending, startTransition] = useTransition();

  const handleThresholdBlur = () => {
    setError(null);
    startTransition(async () => {
      const result = await updateThresholdAction(item.id, Number(threshold));
      if (!result.ok) {
        setError(result.error);
      }
    });
  };

  const handleRemove = () => {
    setError(null);
    startTransition(async () => {
      const result = await removeAction(item.id);
      if (!result.ok) {
        setError(result.error);
      }
    });
  };

  return (
    <TableRow>
      <TableCell>{item.stock_code}</TableCell>
      <TableCell>
        <Input
          value={threshold}
          onChange={(e) => setThreshold(e.target.value)}
          onBlur={handleThresholdBlur}
          disabled={isPending}
          type="number"
          step="0.1"
        />
        {error && <p className="text-sm text-red-500">{error}</p>}
      </TableCell>
      <TableCell>
        <Button variant="destructive" onClick={handleRemove} disabled={isPending}>
          削除
        </Button>
      </TableCell>
    </TableRow>
  );
}
```

- [ ] **Step 8: テストが通ることを確認する**

Run: `npm test -- watchlist-table`
Expected: PASS（全4ケース）

- [ ] **Step 9: ビルドが通ることを確認する**

Run: `npm run build`
Expected: `Compiled successfully`

- [ ] **Step 10: Commit**

```bash
git add frontend/app/watchlist/actions.ts frontend/app/watchlist/actions.test.ts frontend/components/watchlist-table.tsx frontend/components/watchlist-table.test.tsx
git commit -m "feat: add watchlist item removal and threshold update"
```

---

## Task 11: ログアウト機能

**Files:**
- Modify: `frontend/app/watchlist/actions.ts`（`logoutAction` 追加）
- Modify: `frontend/app/watchlist/page.tsx`（`LogoutButton` 追加）
- Create: `frontend/components/logout-button.tsx`
- Test: `frontend/app/watchlist/actions.test.ts`（追記）

**Interfaces:**
- Consumes: `clearSessionToken`（Task4、Task8の`actions.ts`で既にimport済み）
- Produces: `logoutAction(): Promise<void>`、`<LogoutButton />`

- [ ] **Step 1: logoutAction の失敗するテストを追記する**

`frontend/app/watchlist/actions.test.ts` の末尾に追加:

```typescript
import { logoutAction } from "./actions";

describe("logoutAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("clears the session cookie and redirects to /login", async () => {
    await logoutAction();

    expect(session.clearSessionToken).toHaveBeenCalled();
    expect(redirect).toHaveBeenCalledWith("/login");
  });
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `npm test -- app/watchlist/actions`
Expected: FAIL（`logoutAction` が存在しない）

- [ ] **Step 3: `logoutAction` を追加する**

`frontend/app/watchlist/actions.ts` の末尾に追加:

```typescript
export async function logoutAction(): Promise<void> {
  await clearSessionToken();
  redirect("/login");
}
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `npm test -- app/watchlist/actions`
Expected: PASS（全12ケース）

- [ ] **Step 5: LogoutButton を実装する**

`frontend/components/logout-button.tsx`:

```typescript
import { logoutAction } from "@/app/watchlist/actions";
import { Button } from "@/components/ui/button";

export function LogoutButton() {
  return (
    <form action={logoutAction}>
      <Button type="submit" variant="outline">
        ログアウト
      </Button>
    </form>
  );
}
```

- [ ] **Step 6: page.tsx に組み込む**

`frontend/app/watchlist/page.tsx` を編集:

```typescript
import { redirect } from "next/navigation";
import { fetchWatchlist } from "@/lib/go-api-client";
import { WatchlistTable } from "@/components/watchlist-table";
import { AddWatchlistForm } from "@/components/add-watchlist-form";
import { LogoutButton } from "@/components/logout-button";
import { requireToken, redirectIfUnauthorized } from "./actions";

export default async function WatchlistPage() {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    redirect("/login");
  }

  const result = await fetchWatchlist(tokenResult.token);
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    throw new Error(result.error);
  }

  return (
    <main className="mx-auto max-w-2xl p-8">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-2xl font-bold">保有銘柄</h1>
        <LogoutButton />
      </div>
      <AddWatchlistForm />
      <WatchlistTable items={result.items} />
    </main>
  );
}
```

- [ ] **Step 7: ビルドが通ることを確認する**

Run: `npm run build`
Expected: `Compiled successfully`

- [ ] **Step 8: 全テストスイートを通しで実行する**

Run: `npm test`
Expected: 全テストPASS

- [ ] **Step 9: Commit**

```bash
git add frontend/app/watchlist frontend/components/logout-button.tsx
git commit -m "feat: add logout button"
```

---

## 実装後の手動確認（任意）

`frontend/.env.local` に `GO_API_URL=http://localhost:8080` を設定し、Go API（`go-api/`側で `go run ./cmd/api`）とNext.js（`npm run dev`）をローカルで起動して以下を目視確認する:

1. `/register` で新規登録 → `/watchlist` にリダイレクトされる
2. `/watchlist` で銘柄追加・閾値変更・削除ができる
3. ログアウト → `/login` にリダイレクトされ、`/watchlist` に直接アクセスすると `/login` に戻される
4. （任意）Go API側の`JWT_SECRET`を変更してサーバーを再起動し、`/watchlist`にアクセスすると401→`/login`にリダイレクトされることを確認する
