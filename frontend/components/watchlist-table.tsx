"use client";

import { useState, useTransition } from "react";
import { Inbox, Loader2, Trash2 } from "lucide-react";
import type { WatchlistItem } from "@/lib/go-api-client";
import { removeAction } from "@/app/watchlist/actions";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function WatchlistTable({ items }: { items: WatchlistItem[] }) {
  if (items.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-10 text-center text-muted-foreground">
        <Inbox className="size-8" aria-hidden="true" />
        <p className="text-sm">監視銘柄がまだ登録されていません</p>
      </div>
    );
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>証券コード</TableHead>
          <TableHead />
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => (
          <WatchlistRow key={item.id} item={item} />
        ))}
      </TableBody>
    </Table>
  );
}

function WatchlistRow({ item }: { item: WatchlistItem }) {
  const [error, setError] = useState<string | null>(null);
  const [isPending, startTransition] = useTransition();

  const handleRemove = () => {
    setError(null);
    startTransition(async () => {
      const result = await removeAction(item.id);
      if (!result.ok) {
        setError(result.error);
      }
    });
  };

  return (
    <TableRow>
      <TableCell>{item.stock_code}</TableCell>
      <TableCell className="text-right">
        <Button
          variant="destructive"
          size="sm"
          onClick={handleRemove}
          disabled={isPending}
        >
          {isPending ? (
            <Loader2
              data-icon="inline-start"
              className="animate-spin"
              aria-hidden="true"
            />
          ) : (
            <Trash2 data-icon="inline-start" aria-hidden="true" />
          )}
          削除
        </Button>
        {error && (
          <p className="mt-1 text-sm text-destructive" role="alert">
            {error}
          </p>
        )}
      </TableCell>
    </TableRow>
  );
}
