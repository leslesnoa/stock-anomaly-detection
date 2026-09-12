import { TrendingUp } from "lucide-react";
import { cn } from "@/lib/utils";

export function Logo({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
        <TrendingUp className="size-4" aria-hidden="true" />
      </span>
      <span className="text-lg font-semibold tracking-tight text-foreground">
        株価異常検知
      </span>
    </div>
  );
}
