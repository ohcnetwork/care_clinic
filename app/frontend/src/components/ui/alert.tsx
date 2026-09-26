import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";

import { cn } from "@/lib/utils";

// `info` is the design's green explainer box, `warn` its amber "act soon" note,
// `danger` its red "this can break things" warning.
const alertVariants = cva("rounded-md border text-[12.5px] leading-[1.55]", {
  variants: {
    variant: {
      info: "border-brand-line bg-brand-bg px-3 py-2.5 text-brand-ink",
      danger:
        "flex items-center gap-2.5 border-danger-line bg-danger-bg px-3 py-2.5 text-danger-ink",
      warn: "flex items-center gap-2.5 border-warn-line bg-warn-bg px-3 py-2.5 text-warn-ink",
    },
  },
  defaultVariants: { variant: "info" },
});

function Alert({
  className,
  variant,
  ...props
}: React.ComponentProps<"div"> & VariantProps<typeof alertVariants>) {
  return (
    <div
      data-slot="alert"
      role="note"
      className={cn(alertVariants({ variant, className }))}
      {...props}
    />
  );
}

export { Alert };
