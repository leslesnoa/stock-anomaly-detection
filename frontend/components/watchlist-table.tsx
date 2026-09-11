"use client";

import { useState, useTransition } from "react";
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
      <TableCell>
        <Button
          variant="destructive"
          onClick={handleRemove}
          disabled={isPending}
        >
          削除
        </Button>
        {error && <p className="text-sm text-red-500">{error}</p>}
      </TableCell>
    </TableRow>
  );
}
