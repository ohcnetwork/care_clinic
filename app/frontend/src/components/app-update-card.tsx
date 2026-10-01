import { useCallback, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { bridge, onCareEvent } from "@/lib/bridge";
import { errorText, firstLine, megabytes } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { AppUpdate, AppUpdateProgress } from "@/types";

const phaseText = (phase: AppUpdateProgress["phase"], version: string) =>
  ({
    downloading: `Downloading CARE Desktop ${version}...`,
    verifying: `Checking CARE Desktop ${version}...`,
    installing: `Installing CARE Desktop ${version}...`,
    restarting: "Installed. Restarting CARE Desktop...",
    installer: "The installer is open. Follow it to finish updating.",
  })[phase];

export function AppUpdateCard({ disabled = false }: { disabled?: boolean }) {
  const { busy, busyLabel, version, installAppUpdate } = useCare();
  const [update, setUpdate] = useState<AppUpdate | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState("");
  const [progress, setProgress] = useState<AppUpdateProgress | null>(null);
  const [starting, setStarting] = useState(false);

  const check = useCallback(async () => {
    setChecking(true);
    setError("");
    try {
      setUpdate(await bridge.CheckAppUpdate());
    } catch (e) {
      setError(firstLine(errorText(e)));
    } finally {
      setChecking(false);
    }
  }, []);

  useEffect(() => {
    void check();
  }, [check]);

  useEffect(() => {
    const offProgress = onCareEvent("app-update-progress", (next: AppUpdateProgress) => {
      setStarting(false);
      setProgress(next);
    });
    const offDone = onCareEvent("care-done", (code: number, label?: string) => {
      if (label !== "app-update" || code === 0) return;
      setStarting(false);
      setProgress(null);
      setError("CARE Desktop could not finish updating. Your current version was kept. Try again.");
    });
    return () => {
      offProgress();
      offDone();
    };
  }, []);

  const updating = busy && busyLabel === "Updating CARE Desktop";

  useEffect(() => {
    if (!updating && progress?.phase !== "restarting" && progress?.phase !== "installer") {
      setProgress(null);
      setStarting(false);
    }
  }, [updating, progress?.phase]);

  const install = async () => {
    setError("");
    setStarting(true);
    if (!(await installAppUpdate())) setStarting(false);
  };

  const active = starting || updating || progress !== null;

  return (
    <div className="rounded-xl border border-line bg-card px-[18px] py-4 shadow-card">
      <div className="flex items-center gap-3.5">
        <div className="min-w-0 flex-1">
          <div className="text-[15px] font-bold text-ink">CARE Desktop updates</div>
          <div className="mt-[3px] text-[13px] text-muted-foreground">
            Version <span className="font-mono text-ink2">{update?.current || version || "..."}</span>
            {update?.available ? (
              <>
                {" - "}
                <span className="font-semibold text-brand-ink">{update.version} is available</span>
              </>
            ) : update?.version ? (
              " - up to date"
            ) : null}
          </div>
          <p className="mt-1 text-[13px] text-muted-foreground">
            Update this application without setting up a server or connecting to a clinic.
          </p>
        </div>
        <Button disabled={disabled || checking || busy || active} onClick={() => void check()}>
          {checking ? "Checking..." : "Check now"}
        </Button>
      </div>

      {update?.available && active ? (
        <UpdateProgress progress={progress} version={update.version} />
      ) : null}

      {update?.available && !active ? (
        <div className="mt-3.5 flex flex-wrap items-center gap-3 rounded-lg border border-line bg-brand-bg px-4 py-[13px] text-[12.5px] text-brand-ink">
          <span className="min-w-[180px] flex-1">
            Downloads {update.asset}, verifies its checksum, and installs the update.
            CARE Desktop may restart or open an installer. Existing clinic data and
            settings are kept.
          </span>
          {update.notes_url ? (
            <Button onClick={() => void bridge.OpenURL(update.notes_url)}>Release notes</Button>
          ) : null}
          <Button variant="primary" disabled={disabled || busy} onClick={() => void install()}>
            Update CARE Desktop
          </Button>
        </div>
      ) : null}

      {error ? <div role="alert" className="mt-2.5 text-[12.5px] text-danger-ink">{error}</div> : null}
    </div>
  );
}

function UpdateProgress({
  progress,
  version,
}: {
  progress: AppUpdateProgress | null;
  version: string;
}) {
  const phase = progress?.phase ?? "downloading";
  const downloading = phase === "downloading";
  const pct =
    downloading && progress && progress.total > 0
      ? Math.min(100, Math.round((progress.done / progress.total) * 100))
      : undefined;
  const detail =
    downloading && progress && progress.total > 0
      ? `${megabytes(progress.done)} of ${megabytes(progress.total)}`
      : "";

  return (
    <div className="mt-3.5 rounded-lg border border-line bg-brand-bg px-4 py-[13px] text-[12.5px] text-brand-ink">
      <div className="flex items-baseline gap-3">
        <span className="flex-1 font-semibold">{phaseText(phase, version)}</span>
        {detail ? <span className="font-mono text-ink2">{detail}</span> : null}
      </div>
      <Progress className="mt-2.5" value={pct} />
      {phase === "installing" ? (
        <div className="mt-2 text-muted-foreground">
          macOS may ask for an administrator password to replace the app.
        </div>
      ) : null}
    </div>
  );
}
