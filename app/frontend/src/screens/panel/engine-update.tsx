import { RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { EngineState } from "@/types";
import { PanelNotice, usePanelTask } from "./panel-ui";
import { usePanelUpdateLock } from "./panel-update-lock";

/** Reads the CARE engine's version and log collection state, refreshing after every job. */
export function useEngineState() {
  const care = useCare();
  const [engine, setEngine] = useState<EngineState | null>(null);
  useEffect(() => {
    if (care.busy) return;
    let active = true;
    const read = () => bridge.EngineStatus()
      .then((next) => { if (active) setEngine(next); })
      .catch((cause) => care.log(`engine status: ${errorText(cause)}`));
    void read();
    const timer = window.setInterval(read, 15_000);
    return () => { active = false; window.clearInterval(timer); };
  }, [care.busy]);
  return engine;
}

export function EngineUpdateNotice({ engine }: { engine: EngineState | null }) {
  const care = useCare();
  const task = usePanelTask();
  const updateLock = usePanelUpdateLock();
  if (!engine?.supported || !engine.update_needed || care.busy) return null;
  const current = engine.running_version || engine.installed_version;
  const locked = care.busy || task.working || updateLock.active;
  return <PanelNotice title="CARE engine update required" tone="warning" actions={<>
    <Button variant="primary" disabled={locked} onClick={() => {
      if (locked || updateLock.isActive()) return;
      void task.run(() => care.runAction("engine-update"),
        "The CARE engine update didn't start. Try again, or open the logs folder for support.");
    }}><RefreshCw aria-hidden="true" />{engine.update_failed ? "Try the update again" : "Update CARE engine"}</Button>
  </>}>
    This version of CARE Clinic needs CARE engine {engine.expected_version}, but {current ? `${current} is ${engine.running ? "running" : "installed"}` : "an older engine is installed"}.
    {engine.update_failed
      ? <>{" "}The last update didn&apos;t finish and the previous engine was put back{engine.update_error ? `: ${engine.update_error}` : ""}. It isn&apos;t tried again automatically.</>
      : <>{" "}The automatic update didn&apos;t finish, so the clinic is still on the previous engine and engine and container logs are not collected.</>}
    {" "}Updating takes the clinic offline for a few minutes; patient records, files and backups are kept.
    {task.error ? <p className="panel-inline-error" role="alert">{task.error}</p> : null}
  </PanelNotice>;
}

export function engineLogStatus(engine: EngineState | null): { label: string; detail: string; tone: "ok" | "warning" | "neutral" } {
  if (!engine) return { label: "Checking", detail: "Checking whether the CARE engine is collecting logs.", tone: "neutral" };
  if (!engine.supported) return { label: "Docker logs", detail: "This computer runs CARE on Docker directly. Container logs stay in Docker; use docker compose logs.", tone: "neutral" };
  if (engine.update_needed) return { label: "Engine update required", detail: "Engine and container logs are not collected until the CARE engine is updated.", tone: "warning" };
  if (!engine.running) return { label: "Engine stopped", detail: "Engine and container logs are collected while the CARE engine is running.", tone: "neutral" };
  switch (engine.logs) {
    case "collecting": return { label: "Collecting", detail: "Engine and container logs are being written to this folder.", tone: "ok" };
    case "connecting": return { label: "Connecting", detail: "The CARE engine is starting its log stream.", tone: "neutral" };
    case "interrupted": return { label: "Reconnecting", detail: "The log stream was interrupted and is retrying. See collector.log for gaps.", tone: "warning" };
    default: return { label: "Restart needed", detail: "The running CARE engine was started by an older CARE Clinic and can't report log collection. Restart this computer to start collecting.", tone: "warning" };
  }
}
