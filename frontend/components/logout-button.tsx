import { LogOut } from "lucide-react";
import { logoutAction } from "@/app/watchlist/actions";
import { Button } from "@/components/ui/button";

export function LogoutButton() {
  return (
    <form action={logoutAction}>
      <Button type="submit" variant="outline" size="sm">
        <LogOut data-icon="inline-start" aria-hidden="true" />
        ログアウト
      </Button>
    </form>
  );
}
