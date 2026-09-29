"use client";

import { useState } from "react";
import {
  ComposedChart,
  Area,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ReferenceDot,
} from "recharts";
import { Button } from "@/components/ui/button";
import type {
  StockChart as StockChartData,
  StockChartNotification,
} from "@/lib/go-api-client";

export type ChartPeriod = "1M" | "3M" | "6M" | "1Y" | "2Y";

// 各期間ボタンに対応する営業日数。1ヶ月≒21営業日で概算。2Yはバックフィル上限
// （backfillDays=500営業日、go-api側 usecase.BackfillPriceHistoryUsecase）と一致させ、
// 取得済みデータの実質フルレンジを表す。
export const PERIOD_TRADING_DAYS: Record<ChartPeriod, number> = {
  "1M": 21,
  "3M": 63,
  "6M": 126,
  "1Y": 252,
  "2Y": 500,
};

export const PERIOD_OPTIONS: ChartPeriod[] = ["1M", "3M", "6M", "1Y", "2Y"];

export const DEFAULT_PERIOD: ChartPeriod = "6M";

// 表示用に直近days件へ絞り込む。予測の起点（最新の価格・日付）は
// 絞り込み後も変わらない（sliceは末尾を保持するため）ので、buildRowsの予測アンカリングには
// 影響しない。notificationsは絞り込まない: markerRows は絞り込み後のrows（data.prices由来）
// に存在する日付としか一致しないため、絞り込みは自然に反映される。
// daysが実際の件数を超える場合（新規watchlist登録直後などhistoryが浅い場合）は
// Array.prototype.sliceの性質上、全件がそのまま返る。
export function sliceToDisplayWindow(
  data: StockChartData,
  days: number = PERIOD_TRADING_DAYS[DEFAULT_PERIOD],
): StockChartData {
  return {
    ...data,
    prices: data.prices.slice(-days),
    alert_band: data.alert_band.slice(-days),
  };
}

type ChartRow = {
  date: string;
  close?: number;
  // Rechartsの Area は dataKey の値が [min, max] の2要素配列だと、その範囲を帯として描画する
  // （0からの塗りつぶしにならない）。アラート境界・予測レンジ帯はこの仕組みで表現する。
  alertRange?: [number, number];
  forecastCenter?: number;
  forecast68Range?: [number, number];
  forecast95Range?: [number, number];
  notification?: StockChartNotification;
};

// 土日のみを非営業日として扱う簡易実装。日本の祝日カレンダーを持たないため、
// 祝日を挟む場合は予測線のX軸ラベルが実際の取引日と1日以上ずれうる（既知の限界）。
function addBusinessDays(dateStr: string, days: number): string {
  const d = new Date(`${dateStr}T00:00:00Z`);
  let added = 0;
  while (added < days) {
    d.setUTCDate(d.getUTCDate() + 1);
    const day = d.getUTCDay();
    if (day !== 0 && day !== 6) added++;
  }
  return d.toISOString().slice(0, 10);
}

// notified_at（TIMESTAMPTZ、UTC基準でJSON化される）をJSTの暦日に変換する。
// prices[].date はJSTの取引日であり、単純にnotified_atをUTCスライスすると
// POLL_TIME（デフォルト16:00 JST=07:00Z）より前の時刻ではUTC暦日がJSTの前日に
// ずれてしまい、その日のprices[].dateと一致せずマーカーが消える。
function toJstDate(isoString: string): string {
  const d = new Date(isoString);
  const jst = new Date(d.getTime() + 9 * 60 * 60 * 1000);
  return jst.toISOString().slice(0, 10);
}

function buildRows(data: StockChartData): ChartRow[] {
  const alertByDate = new Map(data.alert_band.map((b) => [b.date, b]));
  // 同一日に複数回発火した場合は最新の通知を優先する（末尾優先でMapに詰める）。
  const notificationByDate = new Map(
    data.notifications.map((n) => [toJstDate(n.notified_at), n]),
  );
  const rows: ChartRow[] = data.prices.map((p, i) => {
    const band = alertByDate.get(p.date);
    const isLast = i === data.prices.length - 1;
    return {
      date: p.date,
      close: p.close,
      alertRange: band ? [band.lower, band.upper] : undefined,
      // 予測中心線の起点を実績終値の最終点に重ねて、線がつながって見えるようにする
      // （最終点以外はforecastCenterを持たせない）。
      forecastCenter: isLast ? p.close : undefined,
      notification: notificationByDate.get(p.date),
    };
  });

  if (data.forecast && data.prices.length > 0) {
    const lastDate = data.prices[data.prices.length - 1].date;
    for (const point of data.forecast.points) {
      rows.push({
        date: addBusinessDays(lastDate, point.step),
        forecastCenter: point.center,
        forecast68Range: [point.lower_68, point.upper_68],
        forecast95Range: [point.lower_95, point.upper_95],
      });
    }
  }
  return rows;
}

// 通知履歴があるポイントは zスコアとAIレポート抜粋を、無いポイントは終値のみを表示する。
// デフォルトのTooltipはdataKeyに束ねられた系列しか出せないため、カスタム描画で対応する。
function ChartTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean;
  payload?: { payload: ChartRow }[];
  label?: string;
}) {
  if (!active || !payload || payload.length === 0) return null;
  const row = payload[0].payload;

  return (
    <div className="rounded-md border bg-background p-2 text-sm shadow-sm">
      <p className="font-medium">{label}</p>
      {row.close !== undefined && <p>終値: {row.close.toLocaleString()}</p>}
      {row.notification && (
        <>
          <p>Zスコア: {row.notification.anomaly_score.toFixed(2)}</p>
          <p className="max-w-64 text-muted-foreground">
            {row.notification.ai_report.slice(0, 80)}
            {row.notification.ai_report.length > 80 ? "..." : ""}
          </p>
        </>
      )}
    </div>
  );
}

export function StockChart({ data }: { data: StockChartData }) {
  const [period, setPeriod] = useState<ChartPeriod>(DEFAULT_PERIOD);
  // 表示は選択中の期間に絞る。sliceは末尾（最新）を保持するため、予測のアンカー
  // （data.prices の最終要素の日付・終値）は絞り込み前後で変わらない。
  const displayData = sliceToDisplayWindow(data, PERIOD_TRADING_DAYS[period]);
  const rows = buildRows(displayData);
  const markerRows = rows.filter(
    (r) => r.notification !== undefined && r.close !== undefined,
  );
  const directionModel = data.forecast?.direction_model;

  return (
    <div className="space-y-2">
      <div className="flex gap-1" role="group" aria-label="表示期間">
        {PERIOD_OPTIONS.map((p) => (
          <Button
            key={p}
            type="button"
            size="xs"
            variant={p === period ? "default" : "outline"}
            aria-pressed={p === period}
            onClick={() => setPeriod(p)}
          >
            {p}
          </Button>
        ))}
      </div>
      {!data.forecast && (
        <p className="text-sm text-muted-foreground" role="status">
          予測を取得できませんでした
        </p>
      )}
      <ResponsiveContainer width="100%" height={400}>
        <ComposedChart data={rows}>
          <XAxis dataKey="date" />
          <YAxis domain={["auto", "auto"]} />
          <Tooltip content={<ChartTooltip />} />
          <Legend />
          <Area
            dataKey="alertRange"
            stroke="none"
            fill="#f59e0b"
            fillOpacity={0.15}
            name="アラート境界"
            isAnimationActive={false}
          />
          <Area
            dataKey="forecast95Range"
            stroke="none"
            fill="#6366f1"
            fillOpacity={0.08}
            name="統計的期待レンジ（95%）"
            isAnimationActive={false}
          />
          <Area
            dataKey="forecast68Range"
            stroke="none"
            fill="#6366f1"
            fillOpacity={0.15}
            name="統計的期待レンジ（68%）"
            isAnimationActive={false}
          />
          <Line
            dataKey="close"
            stroke="#0f172a"
            dot={false}
            name="終値"
            isAnimationActive={false}
          />
          <Line
            dataKey="forecastCenter"
            stroke="#6366f1"
            strokeDasharray="4 4"
            dot={false}
            name="予測中心線"
            isAnimationActive={false}
          />
          {markerRows.map((r) => (
            <ReferenceDot
              key={r.date}
              x={r.date}
              y={r.close}
              r={5}
              fill="#ef4444"
              stroke="none"
            />
          ))}
        </ComposedChart>
      </ResponsiveContainer>
      {directionModel?.adopted && directionModel.hit_rate != null && (
        <p className="text-xs text-muted-foreground" role="status">
          AIモデルによる方向予測（過去データでの的中率
          {Math.round((directionModel.hit_rate ?? 0) * 100)}
          %、既存手法比で統計的に有意）。
          この的中率は過去データでの検証結果であり、将来の的中を保証しないことにご注意ください。
        </p>
      )}
      <p className="text-xs text-muted-foreground">
        統計的期待レンジは直近120営業日の対数リターンの平均と標準偏差から算出した
        ドリフト＋ボラティリティ区間であり、価格予測ではありません。アラート境界は
        直近30日の平均±アラート閾値σ（実際に通知が発火する境界）を示します。
      </p>
    </div>
  );
}
