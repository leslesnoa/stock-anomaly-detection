import { logoutAction } from "@/app/watchlist/actions";
import { Button } from "@/components/ui/button";

export function LogoutButton() {
  return (
    <form action={logoutAction}>
      <Button type="submit" variant="outline">
        ログアウト
      </Button>
    </form>
  );
}
