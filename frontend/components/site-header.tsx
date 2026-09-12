import Link from "next/link";
import { Logo } from "@/components/logo";
import { LogoutButton } from "@/components/logout-button";

export function SiteHeader() {
  return (
    <header className="border-b border-border bg-background">
      <div className="mx-auto flex h-14 w-full max-w-3xl items-center justify-between px-6">
        <Link href="/watchlist" aria-label="株価異常検知 ホーム">
          <Logo />
        </Link>
        <LogoutButton />
      </div>
    </header>
  );
}
