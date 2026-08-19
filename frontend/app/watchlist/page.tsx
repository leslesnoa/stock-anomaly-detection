import { redirect } from "next/navigation";
import { fetchWatchlist } from "@/lib/go-api-client";
import { WatchlistTable } from "@/components/watchlist-table";
import { AddWatchlistForm } from "@/components/add-watchlist-form";
import { LogoutButton } from "@/components/logout-button";
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
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-2xl font-bold">保有銘柄</h1>
        <LogoutButton />
      </div>
      <AddWatchlistForm />
      <WatchlistTable items={result.items} />
    </main>
  );
}
