"use client";

import { useEffect, useState } from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import type {
  NewsArticle,
  NewsSentiment,
  NewsSentimentLabel,
  NewsSentimentScores,
} from "@/lib/go-api-client";

export const POLL_INTERVAL_MS = 10_000;
export const MAX_POLLS = 3;
export const INITIAL_VISIBLE_ARTICLES = 5;
export const DISCLAIMER =
  "ニュースタイトルと株価推移に基づくAIの推測です。投資助言ではありません。";

type PanelState =
  | { kind: "loading" }
  | { kind: "pending" }
  | { kind: "gave-up" }
  | { kind: "error" }
  | { kind: "ready"; sentiment: NewsSentiment };

const SCORE_ITEMS: ReadonlyArray<{
  key: keyof NewsSentimentScores;
  label: string;
  barClass: string;
}> = [
  { key: "bullish", label: "勝ち気", barClass: "bg-emerald-600" },
  { key: "bearish", label: "負け気", barClass: "bg-red-600" },
  { key: "impact", label: "インパクト度", barClass: "bg-slate-500" },
  { key: "confidence", label: "確信度", barClass: "bg-slate-500" },
  {
    key: "short_term_up_probability",
    label: "ニュースAI推測: 5営業日後に上昇している確率",
    barClass: "bg-slate-500",
  },
];

const LABELS: Record<NewsSentimentLabel, { text: string; className: string }> =
  {
    bullish: { text: "強気", className: "bg-emerald-100 text-emerald-800" },
    bearish: { text: "弱気", className: "bg-red-100 text-red-800" },
    neutral: { text: "中立", className: "bg-slate-100 text-slate-700" },
  };

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString("ja-JP", { timeZone: "Asia/Tokyo" });
}

function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("ja-JP", {
    timeZone: "Asia/Tokyo",
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function NewsSentimentPanel({ stockCode }: { stockCode: string }) {
  const [state, setState] = useState<PanelState>({ kind: "loading" });

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;

    async function load(polls: number) {
      let sentiment: NewsSentiment;
      try {
        const res = await fetch(
          `/api/stocks/${encodeURIComponent(stockCode)}/news-sentiment`,
          { cache: "no-store", signal: controller.signal },
        );
        if (!res.ok) {
          throw new Error(`status ${res.status}`);
        }
        sentiment = (await res.json()) as NewsSentiment;
      } catch {
        if (!controller.signal.aborted) {
          setState({ kind: "error" });
        }
        return;
      }
      if (controller.signal.aborted) {
        return;
      }
      if (sentiment.status === "pending") {
        if (polls >= MAX_POLLS) {
          setState({ kind: "gave-up" });
          return;
        }
        setState({ kind: "pending" });
        timer = setTimeout(() => void load(polls + 1), POLL_INTERVAL_MS);
        return;
      }
      setState({ kind: "ready", sentiment });
    }

    void load(0);
    return () => {
      controller.abort();
      if (timer) {
        clearTimeout(timer);
      }
    };
  }, [stockCode]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>ニュースとAIセンチメント</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <PanelBody state={state} />
        <p className="text-xs text-muted-foreground">{DISCLAIMER}</p>
      </CardContent>
    </Card>
  );
}

function PanelBody({ state }: { state: PanelState }) {
  switch (state.kind) {
    case "loading":
    case "pending":
      return <p className="text-sm text-muted-foreground">AI分析中…</p>;
    case "gave-up":
      return (
        <p className="text-sm text-muted-foreground">
          時間がかかっています。後で再表示してください
        </p>
      );
    case "error":
      return (
        <p className="text-sm text-destructive">
          ニュースを取得できませんでした
        </p>
      );
    case "ready":
      return <ReadyBody sentiment={state.sentiment} />;
  }
}

function ReadyBody({ sentiment }: { sentiment: NewsSentiment }) {
  if (sentiment.scores === null && sentiment.articles.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        直近90日に判断材料となる開示がありません
      </p>
    );
  }
  return (
    <div className="space-y-4">
      {sentiment.stale && sentiment.scored_at && (
        <p className="text-xs text-amber-700">
          最新の分析ではありません（{formatDateTime(sentiment.scored_at)}時点）
        </p>
      )}
      {sentiment.scores && <ScoreBars scores={sentiment.scores} />}
      <ArticleList articles={sentiment.articles} />
    </div>
  );
}

function ScoreBars({ scores }: { scores: NewsSentimentScores }) {
  return (
    <div className="space-y-2">
      {SCORE_ITEMS.map(({ key, label, barClass }) => {
        const value = scores[key];
        return (
          <div
            key={key}
            role="meter"
            aria-label={label}
            aria-valuenow={value}
            aria-valuemin={0}
            aria-valuemax={100}
          >
            <div className="flex justify-between text-sm">
              <span>{label}</span>
              <span className="tabular-nums">{value}%</span>
            </div>
            <div className="h-2 w-full rounded-full bg-muted">
              <div
                className={`h-2 rounded-full ${barClass}`}
                style={{ width: `${value}%` }}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
}

function ArticleList({ articles }: { articles: NewsArticle[] }) {
  const [expanded, setExpanded] = useState(false);
  const visible = expanded
    ? articles
    : articles.slice(0, INITIAL_VISIBLE_ARTICLES);
  const hidden = articles.length - visible.length;

  return (
    <div className="space-y-2">
      <ul className="divide-y">
        {visible.map((a) => (
          <li
            key={`${a.published_at}-${a.url}-${a.title}`}
            className="flex flex-wrap items-center gap-2 py-2 text-sm"
          >
            <span className="tabular-nums text-muted-foreground">
              {formatDate(a.published_at)}
            </span>
            {a.sentiment && (
              <span
                className={`rounded px-1.5 py-0.5 text-xs font-medium ${LABELS[a.sentiment].className}`}
              >
                {LABELS[a.sentiment].text}
              </span>
            )}
            {a.url ? (
              <a
                href={a.url}
                target="_blank"
                rel="noopener noreferrer"
                className="min-w-0 flex-1 break-words underline-offset-4 hover:underline"
              >
                {a.title}
              </a>
            ) : (
              <span className="min-w-0 flex-1 break-words">{a.title}</span>
            )}
          </li>
        ))}
      </ul>
      {hidden > 0 && (
        <Button variant="ghost" size="sm" onClick={() => setExpanded(true)}>
          もっと見る（残り{hidden}件）
        </Button>
      )}
    </div>
  );
}
