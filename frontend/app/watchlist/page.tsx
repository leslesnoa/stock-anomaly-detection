import { redirect } from "next/navigation";
import { fetchWatchlist } from "@/lib/go-api-client";
import { WatchlistTable } from "@/components/watchlist-table";
import { AddWatchlistForm } from "@/components/add-watchlist-form";
import { LogoutButton } from "@/components/logout-button";
import { requireToken } from "@/lib/auth";

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
