import { redirect } from "next/navigation";
import { fetchWatchlist } from "@/lib/go-api-client";
import { WatchlistTable } from "@/components/watchlist-table";
import { AddWatchlistForm } from "@/components/add-watchlist-form";
import { SiteHeader } from "@/components/site-header";
import { requireToken } from "@/lib/auth";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";

export default async function WatchlistPage() {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    redirect("/login");
  }

  const result = await fetchWatchlist(tokenResult.token);
  if (!result.ok) {
    if (result.status === 401) {
      redirect("/logout");
    }
    throw new Error(result.error);
  }

  return (
    <div className="flex min-h-dvh flex-col bg-muted/20">
      <SiteHeader />
      <main className="mx-auto w-full max-w-3xl flex-1 space-y-6 p-6 md:p-8">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">保有銘柄</h1>
          <p className="text-sm text-muted-foreground">
            監視したい銘柄の証券コードを登録してください。
          </p>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>銘柄を追加</CardTitle>
          </CardHeader>
          <CardContent>
            <AddWatchlistForm />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>監視中の銘柄</CardTitle>
          </CardHeader>
          <CardContent>
            <WatchlistTable items={result.items} />
          </CardContent>
        </Card>
      </main>
    </div>
  );
}
