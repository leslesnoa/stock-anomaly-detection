"use client";

import { useActionState } from "react";
import { addAction, type ActionResult } from "@/app/watchlist/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const initialState: ActionResult | null = null;

export function AddWatchlistForm() {
  const [state, formAction, isPending] = useActionState(
    addAction,
    initialState,
  );

  return (
    <form action={formAction} className="mb-6 flex items-end gap-2">
      <div>
        <Label htmlFor="stock_code">証券コード</Label>
        <Input id="stock_code" name="stock_code" placeholder="7203" required />
      </div>
      <div>
        <Label htmlFor="alert_threshold">閾値</Label>
        <Input
          id="alert_threshold"
          name="alert_threshold"
          type="number"
          step="0.1"
          defaultValue="2.5"
          required
        />
      </div>
      <Button type="submit" disabled={isPending}>
        追加
      </Button>
      {state && !state.ok && (
        <p className="text-sm text-red-500">{state.error}</p>
      )}
    </form>
  );
}
