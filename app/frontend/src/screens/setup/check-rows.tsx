import { Fragment, useEffect, useState } from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { onCareEvent } from "@/lib/bridge";
import { errorText, firstLine } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Check, CheckId } from "./use-requirement-checks";

const DOT: Record<Check["state"], string> = {
  wait: "bg-line text-faint",
  ok: "bg-brand text-white",
  bad: "bg-danger text-white",
};

const GLYPH: Record<Check["state"], string> = { wait: "·", ok: "✓", bad: "✗" };

export function CheckRows({
  checks,
  onDone,
  locked = false,
  onBusyChange,
}: {
  checks: Check[];
  /** Re-run the checks once an action finishes, so the row settles by itself. */
  onDone: () => void;
  locked?: boolean;
  onBusyChange?: (busy: boolean) => void;
}) {
  const [running, setRunning] = useState<CheckId | null>(null);
  const [progress, setProgress] = useState("");
  const [failure, setFailure] = useState<{ id: CheckId; text: string } | null>(null);
  // What the host says to do now that the install finished. Held until the
  // operator dismisses it: an install that ends with "restart Windows first"
  // must not be summarised by the row quietly going red again.
  const [done, setDone] = useState("");

  // Installing Docker means a download of several hundred megabytes. Echoing the
  // engine's log line keeps that from looking like a frozen window.
  useEffect(() => {
    if (!running) return;
    setProgress("");
    return onCareEvent("care-log", (line: unknown) => {
      const text = firstLine(String(line ?? ""));
      if (text) setProgress(text);
    });
  }, [running]);

  const busy = running !== null || done !== "";
  useEffect(() => onBusyChange?.(busy), [busy, onBusyChange]);

  useEffect(() => {
    if (!failure) return;
    const row = checks.find((c) => c.id === failure.id);
    if (!row || row.state === "ok") setFailure(null);
  }, [checks, failure]);

  const perform = (check: Check) => {
    if (!check.action || busy || locked || check.blockedBy) return;
    setRunning(check.id);
    setFailure(null);
    void check.action
      .run()
      .then((message) => {
        if (message) setDone(message);
        else onDone();
      })
      .catch((e) => {
        setFailure({ id: check.id, text: errorText(e) });
        onDone();
      })
      .finally(() => {
        setRunning(null);
        setProgress("");
      });
  };

  return (
    <>
      <AlertDialog open={done !== ""}>
        <AlertDialogContent>
          <AlertDialogTitle>Installed</AlertDialogTitle>
          <AlertDialogDescription className="whitespace-pre-line">{done}</AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogAction
              onClick={() => {
                setDone("");
                onDone();
              }}
            >
              Check again
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <div className="overflow-hidden rounded-lg border border-line">
        {checks.map((check, i) => {
          const working = running === check.id;
          return (
            <Fragment key={check.id}>
              <div
                className={cn(
                  "flex items-center gap-3 px-3.5 py-[13px]",
                  i > 0 && "border-t border-line",
                  check.state === "bad" && "bg-danger-tint",
                )}
              >
                <span
                  className={cn(
                    "flex size-5 flex-none items-center justify-center rounded-full text-[11px] font-bold",
                    DOT[check.state],
                  )}
                >
                  {GLYPH[check.state]}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="text-[13.5px] font-semibold text-ink">{check.title}</div>
                  <div className="mt-px text-[12.5px] text-muted-foreground">{check.detail}</div>
                </div>
                <Badge variant={check.state === "wait" ? "default" : check.state}>
                  {check.state === "wait"
                    ? "Checking"
                    : check.state === "ok"
                      ? "Ready"
                      : "Not ready"}
                </Badge>
              </div>

              {check.state === "bad" && (check.how || check.action) ? (
                <div className="border-t border-danger-bg bg-[#fef6f6] py-3 pr-3.5 pl-[46px]">
                  <div className="flex items-center gap-3">
                    <div className="min-w-0 flex-1 text-[12.5px] leading-[1.5] text-danger-ink">
                      <div>{check.how}</div>
                      {check.action ? (
                        <div className="mt-1 text-muted-foreground">
                          {check.blockedBy
                            ? `Fix ${check.blockedBy} first, then this can be fixed.`
                            : check.action.detail}
                        </div>
                      ) : null}
                    </div>
                    {check.action ? (
                      <Button
                        variant="primary"
                        disabled={busy || locked || check.blockedBy !== undefined}
                        onClick={() => perform(check)}
                      >
                        {working ? <Spinner className="size-3.5" /> : null}
                        {working ? "Working…" : check.action.label}
                      </Button>
                    ) : null}
                  </div>
                  {working && progress ? (
                    <div className="mt-2 truncate font-mono text-[12px] text-muted-foreground">
                      {progress}
                    </div>
                  ) : null}
                  {!working && failure?.id === check.id && running === null ? (
                    <div className="mt-2 text-[12.5px] text-danger-ink">{failure.text}</div>
                  ) : null}
                </div>
              ) : null}
            </Fragment>
          );
        })}
      </div>
    </>
  );
}
