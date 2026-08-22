"use client";

import { useState, useTransition } from "react";
import type { WatchlistItem } from "@/lib/go-api-client";
import { removeAction, updateThresholdAction } from "@/app/watchlist/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
          <TableHead>閾値</TableHead>
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
  const [threshold, setThreshold] = useState(String(item.alert_threshold));
  const [error, setError] = useState<string | null>(null);
  const [isPending, startTransition] = useTransition();

  const handleThresholdBlur = () => {
    setError(null);
    startTransition(async () => {
      const result = await updateThresholdAction(item.id, Number(threshold));
      if (!result.ok) {
        setError(result.error);
      }
    });
  };

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
        <Input
          value={threshold}
          onChange={(e) => setThreshold(e.target.value)}
          onBlur={handleThresholdBlur}
          disabled={isPending}
          type="number"
          step="0.1"
        />
        {error && <p className="text-sm text-red-500">{error}</p>}
      </TableCell>
      <TableCell>
        <Button
          variant="destructive"
          onClick={handleRemove}
          disabled={isPending}
        >
          削除
        </Button>
      </TableCell>
    </TableRow>
  );
}
