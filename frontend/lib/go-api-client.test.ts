import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  registerUser,
  loginUser,
  fetchWatchlist,
  addWatchlistItem,
  removeWatchlistItem,
  fetchStockChart,
  fetchNewsSentiment,
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
        JSON.stringify([
          {
            id: "1",
            stock_code: "7203",
            stock_name: "Toyota Motor Corporation",
            alert_threshold: 2.5,
          },
        ]),
        { status: 200 },
      ),
    );

    const result = await fetchWatchlist("jwt-token");

    expect(result).toEqual({
      ok: true,
      items: [
        {
          id: "1",
          stock_code: "7203",
          stock_name: "Toyota Motor Corporation",
          alert_threshold: 2.5,
        },
      ],
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
        JSON.stringify({
          id: "1",
          stock_code: "7203",
          stock_name: "Toyota Motor Corporation",
          alert_threshold: 2.5,
        }),
        {
          status: 201,
        },
      ),
    );

    const result = await addWatchlistItem("jwt-token", "7203");

    expect(result).toEqual({
      ok: true,
      item: {
        id: "1",
        stock_code: "7203",
        stock_name: "Toyota Motor Corporation",
        alert_threshold: 2.5,
      },
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

  it("fetchStockChart returns chart data and sends bearer token", async () => {
    const chartBody = {
      stock_code: "7203",
      current_price: 3250.0,
      prices: [{ date: "2026-09-01", close: 3200.0 }],
      alert_band: [{ date: "2026-09-01", upper: 3300.0, lower: 3100.0 }],
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
      notifications: [],
    };
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify(chartBody), { status: 200 }),
    );

    const result = await fetchStockChart("jwt-token", "7203");

    expect(result).toEqual({ ok: true, chart: chartBody });
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/stocks/7203/chart",
      expect.objectContaining({
        headers: { Authorization: "Bearer jwt-token" },
      }),
    );
  });

  it("fetchStockChart returns error on 404", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "watchlist item not found" }), {
        status: 404,
      }),
    );

    const result = await fetchStockChart("jwt-token", "9999");

    expect(result).toEqual({
      ok: false,
      error: "watchlist item not found",
      status: 404,
    });
  });

  it("fetchNewsSentiment returns the sentiment and sends bearer token", async () => {
    const body = {
      status: "ready",
      stale: false,
      scores: {
        bullish: 72,
        bearish: 18,
        impact: 55,
        confidence: 40,
        short_term_up_probability: 58,
      },
      scored_by: "claude",
      scored_at: "2026-09-30T06:10:00Z",
      articles: [],
    };
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify(body), { status: 200 }),
    );

    const result = await fetchNewsSentiment("jwt-token", "7203");

    expect(result).toEqual({ ok: true, sentiment: body });
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/stocks/7203/news-sentiment",
      expect.objectContaining({
        headers: { Authorization: "Bearer jwt-token" },
        cache: "no-store",
      }),
    );
  });

  it("fetchNewsSentiment returns status 404 when the stock is not in the watchlist", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "watchlist item not found" }), {
        status: 404,
      }),
    );

    const result = await fetchNewsSentiment("jwt-token", "6758");

    expect(result).toEqual({
      ok: false,
      error: "watchlist item not found",
      status: 404,
    });
  });
});

describe("go-api-client production guard", () => {
  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    vi.resetModules();
  });

  it("does not throw when the module is loaded in production without GO_API_URL", async () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("GO_API_URL", "");

    await expect(import("./go-api-client")).resolves.toBeDefined();
  });

  it("throws when an API call is made in production without GO_API_URL", async () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("GO_API_URL", "");
    vi.stubGlobal("fetch", vi.fn());

    const { registerUser } = await import("./go-api-client");

    await expect(registerUser("a@example.com", "password123")).rejects.toThrow(
      "GO_API_URL must be set in production",
    );
  });
});
