import { notFound, redirect } from "next/navigation";
import { fetchStockChart } from "@/lib/go-api-client";
import { requireToken } from "@/lib/auth";
import { SiteHeader } from "@/components/site-header";
import { StockChart } from "@/components/stock-chart";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";

export default async function StockDetailPage({
  params,
}: {
  params: Promise<{ code: string }>;
}) {
  const { code } = await params;
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    redirect("/login");
  }

  const result = await fetchStockChart(tokenResult.token, code);
  if (!result.ok) {
    if (result.status === 401) {
      redirect("/logout");
    }
    if (result.status === 404) {
      notFound();
    }
    throw new Error(result.error);
  }

  const { chart } = result;

  return (
    <div className="flex min-h-dvh flex-col bg-muted/20">
      <SiteHeader />
      <main className="mx-auto w-full max-w-4xl flex-1 space-y-6 p-6 md:p-8">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {chart.stock_code}
          </h1>
          {chart.current_z_score !== null && (
            <p className="text-sm text-muted-foreground">
              現在のZスコア: {chart.current_z_score.toFixed(2)}
            </p>
          )}
        </div>
        <Card>
          <CardHeader>
            <CardTitle>価格と統計的期待レンジ</CardTitle>
          </CardHeader>
          <CardContent>
            <StockChart data={chart} />
          </CardContent>
        </Card>
      </main>
    </div>
  );
}
