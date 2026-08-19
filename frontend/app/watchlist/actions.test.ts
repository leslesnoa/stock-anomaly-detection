import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/session", () => ({
  getSessionToken: vi.fn(),
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
}));

import { requireToken, redirectIfUnauthorized, addAction } from "./actions";
import * as session from "@/lib/session";
import * as goApiClient from "@/lib/go-api-client";
import { redirect } from "next/navigation";

function watchlistFormData(stockCode: string, threshold: string): FormData {
  const fd = new FormData();
  fd.set("stock_code", stockCode);
  fd.set("alert_threshold", threshold);
  return fd;
}

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
    await redirectIfUnauthorized({
      ok: false,
      error: "missing user context",
      status: 401,
    });

    expect(session.clearSessionToken).toHaveBeenCalled();
    expect(redirect).toHaveBeenCalledWith("/login");
  });

  it("does nothing on a non-401 failure", async () => {
    await redirectIfUnauthorized({
      ok: false,
      error: "internal server error",
      status: 500,
    });

    expect(session.clearSessionToken).not.toHaveBeenCalled();
    expect(redirect).not.toHaveBeenCalled();
  });
});

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
