import { HardDrive, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

import { Badge } from "@/components/ui/badge";
import { diskSize } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { StorageLevel } from "@/types";

const FILL: Record<StorageLevel, string> = {
  ok: "bg-brand",
  low: "bg-warn",
  critical: "bg-danger",
  unknown: "bg-faint",
};

const ICON_TONE: Record<StorageLevel, string> = {
  ok: "bg-brand-bg text-brand-ink",
  low: "bg-warn-bg text-warn-ink",
  critical: "bg-danger-bg text-danger-ink",
  unknown: "bg-hair text-muted-foreground",
};

export const LEVEL_BADGE: Record<
  StorageLevel,
  { variant: "ok" | "warn" | "bad" | "default"; label: string }
> = {
  ok: { variant: "ok", label: "Enough room" },
  low: { variant: "warn", label: "Getting full" },
  critical: { variant: "bad", label: "Full" },
  unknown: { variant: "default", label: "Unknown" },
};

export function StorageMeter({
  free,
  total,
  level,
  className,
}: {
  free: number;
  total: number;
  level: StorageLevel;
  className?: string;
}) {
  const used = total > 0 ? Math.min(100, Math.max(0, ((total - free) / total) * 100)) : 0;
  return (
    <div
      role="meter"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(used)}
      aria-label={`${Math.round(used)}% used`}
      className={cn("h-2 w-full overflow-hidden rounded-full bg-hair", className)}
    >
      <div
        className={cn("h-full rounded-full transition-[width] duration-300", FILL[level])}
        style={{ width: `${used}%` }}
      />
    </div>
  );
}

export function StorageRow({
  label,
  path,
  free,
  total,
  level,
  message,
  note,
  icon: Icon = HardDrive,
  action,
  className,
}: {
  label: string;
  path?: string;
  free: number;
  total: number;
  level: StorageLevel;
  message: string;
  note?: ReactNode;
  icon?: LucideIcon;
  action?: ReactNode;
  className?: string;
}) {
  const badge = LEVEL_BADGE[level];
  return (
    <div className={cn("flex gap-3 px-4 py-3.5", className)}>
      <span
        className={cn(
          "flex size-[30px] flex-none items-center justify-center rounded-sm",
          ICON_TONE[level],
        )}
      >
        <Icon className="size-4" strokeWidth={2} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <div className="min-w-0 flex-1">
            <div className="text-[13.5px] font-semibold text-ink">{label}</div>
            {path ? (
              <div className="truncate font-mono text-[12px] text-muted-foreground" title={path}>
                {path}
              </div>
            ) : null}
          </div>
          <Badge variant={badge.variant} size="sm">
            {badge.label}
          </Badge>
          {action}
        </div>
        <StorageMeter free={free} total={total} level={level} className="mt-2.5" />
        <div className="mt-1.5 flex items-baseline justify-between gap-3 text-[12.5px]">
          <span
            className={cn(
              level === "critical"
                ? "text-danger-ink"
                : level === "low"
                  ? "text-warn-ink"
                  : "text-muted-foreground",
            )}
          >
            {message}
          </span>
          {total > 0 ? (
            <span className="flex-none font-mono text-[12px] text-faint">
              {diskSize(free)} free of {diskSize(total)}
            </span>
          ) : null}
        </div>
        {note ? <div className="mt-1.5 text-[12.5px] text-muted-foreground">{note}</div> : null}
      </div>
    </div>
  );
}
