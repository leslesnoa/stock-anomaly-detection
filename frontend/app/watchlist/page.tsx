import { redirect } from "next/navigation";
import { fetchWatchlist } from "@/lib/go-api-client";
import { WatchlistTable } from "@/components/watchlist-table";
import { AddWatchlistForm } from "@/components/add-watchlist-form";
import { requireToken, redirectIfUnauthorized } from "./actions";

export default async function WatchlistPage() {
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    redirect("/login");
  }

  const result = await fetchWatchlist(tokenResult.token);
  if (!result.ok) {
    await redirectIfUnauthorized(result);
    throw new Error(result.error);
  }

  return (
    <main className="mx-auto max-w-2xl p-8">
      <h1 className="mb-4 text-2xl font-bold">保有銘柄</h1>
      <AddWatchlistForm />
      <WatchlistTable items={result.items} />
    </main>
  );
}
