import { Check, Download, HardDrive, X } from "lucide-react";
import { Fragment, useEffect, useState } from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { onCareEvent } from "@/lib/bridge";
import { diskSize, errorText, firstLine } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { DownloadInfo, PrereqDownloadProgress } from "@/types";
import type { Check as RequirementCheck, CheckId } from "./use-requirement-checks";

const DOT: Record<RequirementCheck["state"], string> = {
  wait: "bg-line text-faint",
  ok: "bg-brand text-white",
  bad: "bg-danger text-white",
};

const GLYPH: Record<RequirementCheck["state"], string> = { wait: "·", ok: "✓", bad: "✗" };

export function CheckRows({
  checks,
  onDone,
  locked = false,
  onBusyChange,
}: {
  checks: RequirementCheck[];
  /** Re-run the checks once an action finishes, so the row settles by itself. */
  onDone: () => void;
  locked?: boolean;
  onBusyChange?: (busy: boolean) => void;
}) {
  const [running, setRunning] = useState<CheckId | null>(null);
  const [progress, setProgress] = useState("");
  const [failure, setFailure] = useState<{ id: CheckId; text: string } | null>(null);
  const [previewing, setPreviewing] = useState<CheckId | null>(null);
  const [confirmation, setConfirmation] = useState<{ check: RequirementCheck; info: DownloadInfo } | null>(null);
  const [download, setDownload] = useState<PrereqDownloadProgress | null>(null);
  // What the host says to do now that the install finished. Held until the
  // operator dismisses it: an install that ends with "restart Windows first"
  // must not be summarised by the row quietly going red again.
  const [done, setDone] = useState("");

  useEffect(() => onCareEvent("prereq-download-progress", (value: PrereqDownloadProgress) => {
    setDownload(value);
  }), []);

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

  const busy = running !== null || previewing !== null || confirmation !== null || done !== "";
  useEffect(() => onBusyChange?.(busy), [busy, onBusyChange]);

  useEffect(() => {
    if (!failure) return;
    const row = checks.find((c) => c.id === failure.id);
    if (!row || row.state === "ok") setFailure(null);
  }, [checks, failure]);

  const execute = (check: RequirementCheck) => {
    if (!check.action) return;
    setConfirmation(null);
    setDownload(null);
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
        setDownload(null);
      });
  };

  const perform = async (check: RequirementCheck) => {
    if (!check.action || busy || locked || check.blockedBy) return;
    if (!check.action.preview) {
      execute(check);
      return;
    }
    setPreviewing(check.id);
    setFailure(null);
    try {
      const info = await check.action.preview();
      setConfirmation({ check, info });
    } catch (e) {
      setFailure({ id: check.id, text: errorText(e) });
    } finally {
      setPreviewing(null);
    }
  };

  return (
    <>
      <AlertDialog open={confirmation !== null} onOpenChange={(open) => {
        if (!open) setConfirmation(null);
      }}>
        <AlertDialogContent>
          <span className="mb-3 flex size-11 items-center justify-center rounded-xl bg-brand-bg text-brand-ink">
            <Download className="size-5" />
          </span>
          <AlertDialogTitle>Download Rancher Desktop?</AlertDialogTitle>
          <AlertDialogDescription>
            CARE needs Rancher Desktop to run the clinic. Review the download size
            before continuing, especially on a limited internet connection.
          </AlertDialogDescription>
          <div className="mt-4 rounded-xl border border-line bg-background p-4">
            <div className="flex items-center justify-between gap-3">
              <span className="flex items-center gap-2 text-[13px] text-muted-foreground">
                <HardDrive className="size-4" /> Download size
              </span>
              <strong className="text-xl text-ink">{confirmation ? diskSize(confirmation.info.size) : ""}</strong>
            </div>
            <p className="mt-2 break-all font-mono text-xs text-muted-foreground">{confirmation?.info.name}</p>
          </div>
          <p className="mt-3 text-[12.5px] text-muted-foreground">
            Nothing has been downloaded yet. Once downloaded, CARE checks the file
            before installing it. Your computer may ask for permission.
          </p>
          <AlertDialogFooter>
            <AlertDialogCancel>Not now</AlertDialogCancel>
            <AlertDialogAction onClick={() => {
              if (confirmation) execute(confirmation.check);
            }}>
              Download and install
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
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
                      ? "Available"
                      : "Needs setup"}
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
                        onClick={() => void perform(check)}
                      >
                        {working || previewing === check.id ? <Spinner className="size-3.5" /> : null}
                        {previewing === check.id ? "Checking size…" : working ? "Working…" : check.action.label}
                      </Button>
                    ) : null}
                  </div>
                  {working && download ? <DownloadStatus download={download} /> : null}
                  {working && progress && (!download || download.phase === "complete") ? (
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

function DownloadStatus({ download }: { download: PrereqDownloadProgress }) {
  const complete = download.phase === "complete";
  const known = download.total > 0;
  const percent = known ? Math.min(100, Math.round(download.done / download.total * 100)) : null;
  const label = {
    connecting: "Connecting to download server",
    downloading: "Downloading installer",
    verifying: "Checking downloaded file",
    complete: "Download verified",
    failed: "Download could not finish",
  }[download.phase];
  return (
    <div className="mt-3 rounded-xl border border-line bg-card p-3.5">
      <div className="mb-2 flex items-center gap-2 text-[13px] font-semibold text-ink" role="status">
        {complete ? <Check className="size-4 text-brand-ink" /> : download.phase === "failed"
          ? <X className="size-4 text-danger-ink" /> : <Spinner className="size-3.5" />}
        <span className="flex-1">{label}</span>
        {download.phase === "downloading" && percent !== null ? <span className="font-mono">{percent}%</span> : null}
      </div>
      <Progress
        value={complete || download.phase === "verifying" ? 100 : download.phase === "connecting" ? null : percent}
        aria-label="Installer download progress"
      />
      <div className="mt-2 flex flex-wrap justify-between gap-1 text-xs text-muted-foreground">
        <span>{diskSize(download.done)}{known ? ` of ${diskSize(download.total)}` : " downloaded"}</span>
        <span>{complete ? "Installing next — keep CARE open" : "Keep this computer connected"}</span>
      </div>
      <p className="mt-1 truncate font-mono text-[11px] text-faint" title={download.name}>{download.name}</p>
    </div>
  );
}
