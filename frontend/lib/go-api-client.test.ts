import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  registerUser,
  loginUser,
  fetchWatchlist,
  addWatchlistItem,
  removeWatchlistItem,
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
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("registerUser returns error message and status on 409", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "email already exists" }), {
        status: 409,
      }),
    );

    const result = await registerUser("test@example.com", "password123");

    expect(result).toEqual({
      ok: false,
      error: "email already exists",
      status: 409,
    });
  });

  it("loginUser returns token on 200", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ token: "jwt-token" }), { status: 200 }),
    );

    const result = await loginUser("test@example.com", "password123");

    expect(result).toEqual({ ok: true, token: "jwt-token" });
  });

  it("loginUser returns error message and status on 401", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "invalid credentials" }), {
        status: 401,
      }),
    );

    const result = await loginUser("test@example.com", "wrong-password");

    expect(result).toEqual({
      ok: false,
      error: "invalid credentials",
      status: 401,
    });
  });

  it("fetchWatchlist returns items and sends bearer token", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify([{ id: "1", stock_code: "7203", alert_threshold: 2.5 }]),
        { status: 200 },
      ),
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
      }),
    );
  });

  it("fetchWatchlist returns status 401 when the token is invalid", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "missing user context" }), {
        status: 401,
      }),
    );

    const result = await fetchWatchlist("expired-token");

    expect(result).toEqual({
      ok: false,
      error: "missing user context",
      status: 401,
    });
  });

  it("addWatchlistItem returns created item on 201", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({ id: "1", stock_code: "7203", alert_threshold: 2.5 }),
        {
          status: 201,
        },
      ),
    );

    const result = await addWatchlistItem("jwt-token", "7203");

    expect(result).toEqual({
      ok: true,
      item: { id: "1", stock_code: "7203", alert_threshold: 2.5 },
    });
  });

  it("addWatchlistItem returns error message and status on 409", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "stock already in watchlist" }), {
        status: 409,
      }),
    );

    const result = await addWatchlistItem("jwt-token", "7203");

    expect(result).toEqual({
      ok: false,
      error: "stock already in watchlist",
      status: 409,
    });
  });

  it("removeWatchlistItem returns ok on 204", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));

    const result = await removeWatchlistItem("jwt-token", "1");

    expect(result).toEqual({ ok: true });
  });

  it("removeWatchlistItem returns error message and status on 404", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "watchlist item not found" }), {
        status: 404,
      }),
    );

    const result = await removeWatchlistItem("jwt-token", "1");

    expect(result).toEqual({
      ok: false,
      error: "watchlist item not found",
      status: 404,
    });
  });
});
