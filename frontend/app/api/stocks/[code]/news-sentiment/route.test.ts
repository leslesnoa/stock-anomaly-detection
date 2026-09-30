import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/lib/auth", () => ({
  requireToken: vi.fn(),
}));
vi.mock("@/lib/go-api-client", () => ({
  fetchNewsSentiment: vi.fn(),
}));

import { GET } from "./route";
import * as auth from "@/lib/auth";
import * as goApiClient from "@/lib/go-api-client";

function context(code: string) {
  return { params: Promise.resolve({ code }) };
}

const request = new Request(
  "http://localhost:3000/api/stocks/7203/news-sentiment",
);

describe("GET /api/stocks/[code]/news-sentiment", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns 401 without calling go-api when not logged in", async () => {
    vi.mocked(auth.requireToken).mockResolvedValue({
      ok: false,
      error: "unauthorized",
    });

    const response = await GET(request, context("7203"));

    expect(response.status).toBe(401);
    expect(goApiClient.fetchNewsSentiment).not.toHaveBeenCalled();
  });

  it("returns the sentiment from go-api", async () => {
    vi.mocked(auth.requireToken).mockResolvedValue({
      ok: true,
      token: "jwt-token",
    });
    const sentiment = {
      status: "pending" as const,
      stale: false,
      scores: null,
      scored_by: null,
      scored_at: null,
      articles: [],
    };
    vi.mocked(goApiClient.fetchNewsSentiment).mockResolvedValue({
      ok: true,
      sentiment,
    });

    const response = await GET(request, context("7203"));

    expect(goApiClient.fetchNewsSentiment).toHaveBeenCalledWith(
      "jwt-token",
      "7203",
    );
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual(sentiment);
  });

  it("passes through the go-api error status", async () => {
    vi.mocked(auth.requireToken).mockResolvedValue({
      ok: true,
      token: "jwt-token",
    });
    vi.mocked(goApiClient.fetchNewsSentiment).mockResolvedValue({
      ok: false,
      error: "watchlist item not found",
      status: 404,
    });

    const response = await GET(request, context("6758"));

    expect(response.status).toBe(404);
    expect(await response.json()).toEqual({
      error: "watchlist item not found",
    });
  });
});
