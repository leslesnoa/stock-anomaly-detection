import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  act,
} from "@testing-library/react";
import {
  NewsSentimentPanel,
  POLL_INTERVAL_MS,
  MAX_POLLS,
  DISCLAIMER,
} from "./news-sentiment-panel";
import type { NewsArticle, NewsSentiment } from "@/lib/go-api-client";

function article(i: number, overrides: Partial<NewsArticle> = {}): NewsArticle {
  return {
    title: `開示${i}`,
    url: `https://example.com/${i}.pdf`,
    published_at: `2026-09-${String(10 + i).padStart(2, "0")}T06:30:00Z`,
    sentiment: "bullish",
    sentiment_confidence: 80,
    ...overrides,
  };
}

function ready(overrides: Partial<NewsSentiment> = {}): NewsSentiment {
  return {
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
    articles: [article(1)],
    ...overrides,
  };
}

const pending: NewsSentiment = {
  status: "pending",
  stale: false,
  scores: null,
  scored_by: null,
  scored_at: null,
  articles: [],
};

// 本物の Response.json() はストリーム読み取りの非同期I/Oを挟み、偽タイマーでの時間送りと
// 噛み合わないことがある。マイクロタスクだけで解決する最小限のスタブにする。
function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as unknown as Response;
}

describe("NewsSentimentPanel", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("fetches from the route handler and shows scores, articles and the disclaimer", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(ready()));

    render(<NewsSentimentPanel stockCode="7203" />);

    expect(await screen.findByText("開示1")).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith(
      "/api/stocks/7203/news-sentiment",
      expect.objectContaining({ cache: "no-store" }),
    );
    expect(screen.getByRole("meter", { name: "勝ち気" })).toHaveAttribute(
      "aria-valuenow",
      "72",
    );
    expect(screen.getByRole("meter", { name: "負け気" })).toHaveAttribute(
      "aria-valuenow",
      "18",
    );
    expect(screen.getByRole("meter", { name: "インパクト度" })).toHaveAttribute(
      "aria-valuenow",
      "55",
    );
    expect(screen.getByRole("meter", { name: "確信度" })).toHaveAttribute(
      "aria-valuenow",
      "40",
    );
    expect(
      screen.getByRole("meter", {
        name: "ニュースAI推測: 5営業日後に上昇している確率",
      }),
    ).toHaveAttribute("aria-valuenow", "58");
    expect(screen.getByText("強気")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "開示1" })).toHaveAttribute(
      "href",
      "https://example.com/1.pdf",
    );
    expect(screen.getByText(DISCLAIMER)).toBeInTheDocument();
  });

  it("shows the first 5 articles and expands the rest", async () => {
    const articles = [1, 2, 3, 4, 5, 6, 7].map((i) => article(i));
    vi.mocked(fetch).mockResolvedValue(jsonResponse(ready({ articles })));

    render(<NewsSentimentPanel stockCode="7203" />);

    expect(await screen.findByText("開示5")).toBeInTheDocument();
    expect(screen.queryByText("開示6")).not.toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "もっと見る（残り2件）" }),
    );

    expect(screen.getByText("開示7")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /もっと見る/ }),
    ).not.toBeInTheDocument();
  });

  it("shows badges per label and no badge for unscored articles", async () => {
    vi.mocked(fetch).mockResolvedValue(
      jsonResponse(
        ready({
          articles: [
            article(1, { sentiment: "bearish" }),
            article(2, { sentiment: "neutral" }),
            article(3, { sentiment: null, sentiment_confidence: null }),
          ],
        }),
      ),
    );

    render(<NewsSentimentPanel stockCode="7203" />);

    expect(await screen.findByText("弱気")).toBeInTheDocument();
    expect(screen.getByText("中立")).toBeInTheDocument();
    expect(screen.queryByText("強気")).not.toBeInTheDocument();
  });

  it("renders a title without a link when the url is empty", async () => {
    vi.mocked(fetch).mockResolvedValue(
      jsonResponse(ready({ articles: [article(1, { url: "" })] })),
    );

    render(<NewsSentimentPanel stockCode="7203" />);

    expect(await screen.findByText("開示1")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "開示1" }),
    ).not.toBeInTheDocument();
  });

  it("shows a stale notice", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(ready({ stale: true })));

    render(<NewsSentimentPanel stockCode="7203" />);

    expect(
      await screen.findByText(/最新の分析ではありません/),
    ).toBeInTheDocument();
  });

  it("shows a message when there are no disclosures", async () => {
    vi.mocked(fetch).mockResolvedValue(
      jsonResponse(ready({ scores: null, articles: [] })),
    );

    render(<NewsSentimentPanel stockCode="7203" />);

    expect(
      await screen.findByText("直近90日に判断材料となる開示がありません"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("meter")).not.toBeInTheDocument();
  });

  it("shows an error message when the request fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      jsonResponse({ error: "internal server error" }, 500),
    );

    render(<NewsSentimentPanel stockCode="7203" />);

    expect(
      await screen.findByText("ニュースを取得できませんでした"),
    ).toBeInTheDocument();
    expect(screen.queryByText("internal server error")).not.toBeInTheDocument();
    expect(screen.getByText(DISCLAIMER)).toBeInTheDocument();
  });

  it("polls while pending and shows the result when it becomes ready", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(fetch)
      .mockResolvedValueOnce(jsonResponse(pending))
      .mockResolvedValueOnce(jsonResponse(ready()));

    render(<NewsSentimentPanel stockCode="7203" />);

    expect(await screen.findByText("AI分析中…")).toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });

    expect(await screen.findByText("開示1")).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it("gives up after MAX_POLLS re-fetches", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(fetch).mockImplementation(async () => jsonResponse(pending));

    render(<NewsSentimentPanel stockCode="7203" />);

    for (let i = 0; i < MAX_POLLS; i++) {
      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(i + 1));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
      });
    }

    expect(
      await screen.findByText("時間がかかっています。後で再表示してください"),
    ).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(1 + MAX_POLLS);
  });

  it("stops polling after unmount", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(fetch).mockImplementation(async () => jsonResponse(pending));

    const { unmount } = render(<NewsSentimentPanel stockCode="7203" />);
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    await act(async () => {
      await Promise.resolve();
    });

    unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * (MAX_POLLS + 1));
    });

    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
