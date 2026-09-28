import { useCallback, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { bridge, onCareEvent } from "@/lib/bridge";
import { errorText, firstLine, megabytes } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { AppUpdate, AppUpdateProgress, CareCheck, ChannelStatus } from "@/types";

const short = (sha: string) => (sha ? sha.slice(0, 8) : "");

export function UpdatePanel() {
  return (
    <div className="flex flex-col gap-3">
      <CareChannelCard />
      <AppUpdateCard />
    </div>
  );
}

function CareChannelCard() {
  const { log, busy, careUpdate, applyCareUpdate } = useCare();
  const [status, setStatus] = useState<ChannelStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const [upToDate, setUpToDate] = useState(false);
  const [error, setError] = useState("");

  const reload = useCallback(async () => {
    try {
      setStatus(await bridge.CareUpdateStatus());
      setError("");
    } catch (e) {
      setError(firstLine(errorText(e)));
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload, careUpdate, busy]);

  useEffect(
    () =>
      onCareEvent("care-check", (check: CareCheck) => {
        setChecking(check.running);
        setUpToDate(!check.running && !check.found);
        if (!check.running) setError("");
      }),
    [],
  );

  const check = async () => {
    setChecking(true);
    setUpToDate(false);
    setError("");
    try {
      await bridge.CheckCareUpdate();
      log("Checking for CARE updates in the background...");
    } catch (e) {
      setError(firstLine(errorText(e)));
      setChecking(false);
    }
  };

  const pending = status?.pending_backend || status?.pending_frontend || "";

  return (
    <div className="rounded-xl border border-line bg-card px-[18px] py-4 shadow-card">
      <div className="flex items-center gap-3.5">
        <div className="min-w-0 flex-1">
          <div className="text-[15px] font-bold text-ink">CARE</div>
          <div className="mt-[3px] text-[13px] text-muted-foreground">
            Follows the{" "}
            <span className="font-mono text-ink2">{status?.backend_branch || "…"}</span> branch and
            updates itself. New versions are downloaded and built in the background.
          </div>
        </div>
        <Button disabled={checking || busy} onClick={() => void check()}>
          {checking ? "Checking…" : "Check now"}
        </Button>
      </div>

      <dl className="mt-3.5 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-hair pt-3.5 text-[13px]">
        <Row label="Backend" value={short(status?.backend ?? "")} />
        <Row label="Frontend" value={short(status?.frontend ?? "")} />
      </dl>

      {!pending && checking ? (
        <div className="mt-3.5 text-[12.5px] text-muted-foreground">
          Checking for updates. Anything found is downloaded and built in the background,
          which can take a few minutes.
        </div>
      ) : null}

      {!pending && !checking && upToDate ? (
        <div className="mt-3.5 text-[12.5px] text-muted-foreground">
          Up to date with the <span className="font-mono text-ink2">{status?.backend_branch}</span>{" "}
          branch.
        </div>
      ) : null}

      {pending ? (
        <div className="mt-3.5 flex items-center gap-3 rounded-lg border border-line bg-brand-bg px-4 py-[13px] text-[12.5px] text-brand-ink">
          <span className="flex-1">
            An update is built and ready. It installs on the next start, or now.
          </span>
          <Button variant="primary" disabled={busy} onClick={() => void applyCareUpdate()}>
            Install now
          </Button>
        </div>
      ) : null}

      {error ? <div className="mt-2.5 text-[12.5px] text-danger-ink">{error}</div> : null}
    </div>
  );
}

const phaseText = (phase: AppUpdateProgress["phase"], version: string) =>
  ({
    downloading: `Downloading CARE Desktop ${version}…`,
    verifying: `Checking CARE Desktop ${version}…`,
    installing: `Installing CARE Desktop ${version}…`,
    restarting: "Installed. Restarting CARE Desktop…",
    installer: "The installer is open. Follow it to finish updating.",
  })[phase];

function AppUpdateCard() {
  const { busy, busyLabel, installAppUpdate } = useCare();
  const [update, setUpdate] = useState<AppUpdate | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState("");
  const [progress, setProgress] = useState<AppUpdateProgress | null>(null);
  const [starting, setStarting] = useState(false);

  const check = useCallback(async (announce: boolean) => {
    setChecking(true);
    setError("");
    try {
      setUpdate(await bridge.CheckAppUpdate());
    } catch (e) {
      if (announce) setError(firstLine(errorText(e)));
    } finally {
      setChecking(false);
    }
  }, []);

  useEffect(() => {
    void check(false);
  }, [check]);

  useEffect(
    () =>
      onCareEvent("app-update-progress", (next: AppUpdateProgress) => {
        setStarting(false);
        setProgress(next);
      }),
    [],
  );

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
    await installAppUpdate();
  };

  const active = starting || progress !== null;

  return (
    <div className="rounded-xl border border-line bg-card px-[18px] py-4 shadow-card">
      <div className="flex items-center gap-3.5">
        <div className="min-w-0 flex-1">
          <div className="text-[15px] font-bold text-ink">CARE Desktop</div>
          <div className="mt-[3px] text-[13px] text-muted-foreground">
            This application. Version{" "}
            <span className="font-mono text-ink2">{update?.current || "…"}</span>
            {update?.available ? (
              <>
                {" — "}
                <span className="font-semibold text-brand-ink">
                  {update.version} is available
                </span>
              </>
            ) : update?.version ? (
              " — up to date"
            ) : null}
          </div>
        </div>
        <Button disabled={checking || busy || active} onClick={() => void check(true)}>
          {checking ? "Checking…" : "Check now"}
        </Button>
      </div>

      {update?.available && active ? (
        <UpdateProgress progress={progress} version={update.version} />
      ) : null}

      {update?.available && !active ? (
        <div className="mt-3.5 flex items-center gap-3 rounded-lg border border-line bg-brand-bg px-4 py-[13px] text-[12.5px] text-brand-ink">
          <span className="flex-1">
            Downloads {update.asset}, checks it against the published checksum, then
            installs it and restarts CARE Desktop. Patient data and clinic settings are
            untouched, and the clinic keeps running.
          </span>
          {update.notes_url ? (
            <Button onClick={() => void bridge.OpenURL(update.notes_url)}>Release notes</Button>
          ) : null}
          <Button variant="primary" disabled={busy} onClick={() => void install()}>
            Update and restart
          </Button>
        </div>
      ) : null}

      {error ? <div className="mt-2.5 text-[12.5px] text-danger-ink">{error}</div> : null}
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
      : null;
  const detail =
    downloading && progress && progress.total > 0
      ? `${megabytes(progress.done)} of ${megabytes(progress.total)}`
      : "";

  return (
    <div className="mt-3.5 rounded-lg border border-line bg-brand-bg px-4 py-[13px] text-[12.5px] text-brand-ink">
      <div className="flex items-baseline gap-3">
        <span className="flex-1 font-semibold">
          {phaseText(phase, version)}
        </span>
        {detail ? <span className="font-mono text-ink2">{detail}</span> : null}
      </div>
      <Progress
        className={pct === null ? "mt-2.5 animate-pulse" : "mt-2.5"}
        value={pct ?? 100}
      />
      {phase === "installing" ? (
        <div className="mt-2 text-muted-foreground">
          macOS may ask for an administrator password to replace the app.
        </div>
      ) : null}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-mono font-semibold text-ink">{value || "not built yet"}</dd>
    </div>
  );
}
