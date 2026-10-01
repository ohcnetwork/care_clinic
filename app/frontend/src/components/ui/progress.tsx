import * as ProgressPrimitive from "@radix-ui/react-progress";
import type * as React from "react";

import { cn } from "@/lib/utils";

function Progress({
  className,
  value,
  ...props
}: React.ComponentProps<typeof ProgressPrimitive.Root>) {
  return (
    <ProgressPrimitive.Root
      data-slot="progress"
      value={value}
      className={cn("h-[9px] w-full overflow-hidden rounded-full bg-line", className)}
      {...props}
    >
      <ProgressPrimitive.Indicator
        data-slot="progress-indicator"
        className={cn(
          "h-full rounded-full bg-brand transition-[width] duration-[350ms] ease-out",
          value == null && "animate-pulse",
        )}
        style={{ width: value == null ? "35%" : `${value}%` }}
      />
    </ProgressPrimitive.Root>
  );
}

export { Progress };
