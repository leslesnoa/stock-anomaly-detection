import type { WatchlistItem } from "@/lib/go-api-client";
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
          <TableRow key={item.id}>
            <TableCell>{item.stock_code}</TableCell>
            <TableCell>{item.alert_threshold}</TableCell>
            <TableCell />
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
