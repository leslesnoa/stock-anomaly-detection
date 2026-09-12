"use client";

import { useActionState } from "react";
import { Hash, Loader2, Plus } from "lucide-react";
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
    <form
      action={formAction}
      className="flex flex-col gap-3 sm:flex-row sm:items-end sm:gap-2"
    >
      <div className="flex-1 space-y-1.5">
        <Label htmlFor="stock_code">証券コード</Label>
        <div className="relative">
          <Hash
            className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <Input
            id="stock_code"
            name="stock_code"
            placeholder="7203"
            required
            className="pl-8"
          />
        </div>
      </div>
      <Button type="submit" disabled={isPending}>
        {isPending ? (
          <Loader2
            data-icon="inline-start"
            className="animate-spin"
            aria-hidden="true"
          />
        ) : (
          <Plus data-icon="inline-start" aria-hidden="true" />
        )}
        追加
      </Button>
      {state && !state.ok && (
        <p className="text-sm text-destructive sm:basis-full" role="alert">
          {state.error}
        </p>
      )}
    </form>
  );
}
