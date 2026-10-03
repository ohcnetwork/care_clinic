import { useCallback, useEffect, useRef, useState } from "react";

import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { DiskStatus, DockerStatus, NetworkStatus, ResidueReport, RestartPlan, ToolPlan, WSLStatus } from "@/types";
import type { RequirementPage, StepState } from "./setup-model";

export type SoftwareStatus = {
  docker: DockerStatus; git: DockerStatus;
  dockerPlan: ToolPlan | null; gitPlan: ToolPlan | null;
};
type Values = {
  space: DiskStatus;
  windows: { status: WSLStatus; restart: RestartPlan };
  software: SoftwareStatus;
  cleanup: ResidueReport;
  network: NetworkStatus;
};
export type SetupChecks = {
  [K in RequirementPage]: { state: StepState; value: Values[K] | null; error: string };
};
const EMPTY: SetupChecks = {
  space: { state: "waiting", value: null, error: "" },
  windows: { state: "waiting", value: null, error: "" },
  software: { state: "waiting", value: null, error: "" },
  cleanup: { state: "waiting", value: null, error: "" },
  network: { state: "waiting", value: null, error: "" },
};

export function useSetupChecks() {
  const { log } = useCare();
  const [checks, setChecks] = useState<SetupChecks>(EMPTY);
  const current = useRef(checks);
  const pending = useRef<Partial<Record<RequirementPage, Promise<boolean>>>>({});
  const mounted = useRef(false);

  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const update = useCallback(<K extends RequirementPage>(id: K, result: SetupChecks[K]) => {
    current.current = { ...current.current, [id]: result };
    if (mounted.current) setChecks(current.current);
  }, []);

  const check = useCallback((id: RequirementPage): Promise<boolean> => {
    const running = pending.current[id];
    if (running) return running;
    update(id, { ...current.current[id], state: "checking", error: "" });
    const task = (async () => {
      try {
        switch (id) {
          case "space": {
            const value = await bridge.DiskStatus();
            const ok = value.ok && value.need > 0 && value.free >= value.need;
            update("space", { value, state: ok ? "ready" : "blocked", error: value.need === 0 ? "Couldn't measure the free space on this computer. Check again before continuing." : "" });
            return ok;
          }
          case "windows": {
            const [status, restart] = await Promise.all([bridge.WSLStatus(), bridge.RestartPlan()]);
            const ok = !status.applicable || (status.ok && !restart.needed);
            update("windows", { value: { status, restart }, state: ok ? "ready" : "blocked", error: "" });
            return ok;
          }
          case "software": {
            const [docker, git] = await Promise.all([bridge.DockerStatus(), bridge.GitStatus()]);
            const [dockerPlan, gitPlan] = await Promise.all([
              docker.ok ? null : bridge.DockerPlan(), git.ok ? null : bridge.GitPlan(),
            ]);
            const ok = docker.ok && git.ok;
            update("software", { value: { docker, git, dockerPlan, gitPlan }, state: ok ? "ready" : "blocked", error: "" });
            return ok;
          }
          case "cleanup": {
            const value = await bridge.ScanResidue();
            update("cleanup", { value, state: value.clean ? "ready" : "blocked", error: "" });
            return value.clean;
          }
          case "network": {
            const value = await bridge.NetworkStatus();
            const ok = !value.applicable || value.ok;
            update("network", { value, state: ok ? "ready" : "blocked", error: "" });
            return ok;
          }
        }
      } catch (e) {
        log(`setup ${id}: ${errorText(e)}`);
        update(id, { ...current.current[id], state: "failed", error: "This step couldn't be checked. Try again. If it keeps happening, share the log file with your support contact." });
        return false;
      } finally { delete pending.current[id]; }
    })();
    pending.current[id] = task;
    return task;
  }, [log, update]);

  return { checks, current, check };
}
