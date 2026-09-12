import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/session", () => ({
  clearSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn(),
}));
vi.mock("next/cache", () => ({
  revalidatePath: vi.fn(),
}));
vi.mock("@/lib/go-api-client", () => ({
  addWatchlistItem: vi.fn(),
  removeWatchlistItem: vi.fn(),
}));
vi.mock("@/lib/auth", () => ({
  requireToken: vi.fn(),
  redirectIfUnauthorized: vi.fn(),
}));

import { addAction, removeAction, logoutAction } from "./actions";
import * as session from "@/lib/session";
import * as goApiClient from "@/lib/go-api-client";
import * as auth from "@/lib/auth";
import { redirect } from "next/navigation";

function watchlistFormData(stockCode: string): FormData {
  const fd = new FormData();
  fd.set("stock_code", stockCode);
  return fd;
}

describe("addAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(auth.requireToken).mockResolvedValue({
      ok: true,
      token: "jwt-token",
    });
  });

  it("returns ok on success", async () => {
    vi.mocked(goApiClient.addWatchlistItem).mockResolvedValue({
      ok: true,
      item: { id: "1", stock_code: "7203", alert_threshold: 2.5 },
    });

    const result = await addAction(null, watchlistFormData("7203"));

    expect(result).toEqual({ ok: true });
  });

  it("returns the error message when the API call fails", async () => {
    vi.mocked(goApiClient.addWatchlistItem).mockResolvedValue({
      ok: false,
      error: "stock already in watchlist",
      status: 409,
    });

    const result = await addAction(null, watchlistFormData("7203"));

    expect(result).toEqual({ ok: false, error: "stock already in watchlist" });
  });

  it("calls redirectIfUnauthorized when the API call returns 401", async () => {
    const failure = {
      ok: false as const,
      error: "missing user context",
      status: 401,
    };
    vi.mocked(goApiClient.addWatchlistItem).mockResolvedValue(failure);

    await addAction(null, watchlistFormData("7203"));

    expect(auth.redirectIfUnauthorized).toHaveBeenCalledWith(failure);
  });
});

describe("removeAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(auth.requireToken).mockResolvedValue({
      ok: true,
      token: "jwt-token",
    });
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

  it("calls redirectIfUnauthorized when the API call returns 401", async () => {
    const failure = {
      ok: false as const,
      error: "missing user context",
      status: 401,
    };
    vi.mocked(goApiClient.removeWatchlistItem).mockResolvedValue(failure);

    await removeAction("1");

    expect(auth.redirectIfUnauthorized).toHaveBeenCalledWith(failure);
  });
});

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
