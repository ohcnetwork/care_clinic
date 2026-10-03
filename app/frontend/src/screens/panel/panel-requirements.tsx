import { Check, Download, RefreshCw, X } from "lucide-react";
import {
  createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode,
} from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { bridge, onCareEvent } from "@/lib/bridge";
import { diskSize, errorText } from "@/lib/format";
import { downloadProblem, type PrerequisiteProblem } from "@/lib/prerequisite-errors";
import { RestartDialog } from "@/screens/setup/restart-dialog";
import { SOFTWARE_DESCRIPTIONS } from "@/screens/setup/setup-model";
import { useCare } from "@/state/care-store";
import type { DownloadInfo, PrereqDownloadProgress, RestartPlan, ToolPlan } from "@/types";
import { PanelBadge, PanelLogButton, PanelNotice } from "./panel-ui";
import { usePanelUpdateLock } from "./panel-update-lock";

type CheckId = "wsl" | "docker" | "git" | "network" | "disk" | "address" | "clinic";
type CheckState = "ready" | "bad" | "unknown" | "waiting";
type Fix = {
  label: string;
  run: () => Promise<unknown>;
  preview?: boolean;
  progress?: string;
};
export type PanelCheck = {
  id: CheckId;
  title: string;
  detail: string;
  state: CheckState;
  action?: Fix;
  blocked?: string;
};

const TITLES: Record<CheckId, string> = {
  wsl: "WSL 2", docker: "Rancher Desktop", git: "Git", network: "Network profile",
  disk: "Enough free space", address: "Clinic address", clinic: "Clinic software",
};
const CORE = new Set<CheckId>(["wsl", "docker", "git", "network"]);

function useChecks() {
  const { platform, mdnsName, busy, busyLabel, operationError, restorePending, runAction, refresh, setTab, log } = useCare();
  const updateLock = usePanelUpdateLock();
  const [checks, setChecks] = useState<PanelCheck[]>([]);
  const [checking, setChecking] = useState(false);
  const [checkedAt, setCheckedAt] = useState(0);
  const [running, setRunning] = useState<CheckId | null>(null);
  const [target, setTarget] = useState<CheckId | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [confirmation, setConfirmation] = useState<{ check: PanelCheck; info: DownloadInfo } | null>(null);
  const [failure, setFailure] = useState<PrerequisiteProblem | null>(null);
  const [download, setDownload] = useState<PrereqDownloadProgress | null>(null);
  const [restart, setRestart] = useState<RestartPlan | null>(null);
  const mounted = useRef(false);
  const pending = useRef<Promise<void> | null>(null);
  const fixing = useRef(false);
  const executing = useRef(false);
  const busyRef = useRef(busy);
  busyRef.current = busy;

  const toolFix = useCallback((id: "docker" | "git", plan?: ToolPlan): Fix | undefined => {
    if (!plan || !plan.action) return undefined;
    if (plan.action === "manual") return plan.url ? {
      label: `Open ${id === "docker" ? "Rancher Desktop" : "Git"} download page`,
      run: () => bridge.OpenURL(plan.url),
    } : undefined;
    if (plan.action === "open") return id === "docker"
      ? {
        label: "Start Rancher Desktop", run: () => bridge.OpenDocker(),
        progress: "Starting Rancher Desktop — this can take a minute.",
      }
      : undefined;
    return {
      label: `Install ${id === "docker" ? "Rancher Desktop" : "Git"}`,
      run: () => id === "docker" ? bridge.InstallDocker() : bridge.InstallGit(),
      preview: id === "docker" && plan.download_preview,
    };
  }, []);

  const recheck = useCallback((): Promise<void> => {
    if (pending.current) return pending.current;
    if (busyRef.current || fixing.current) return Promise.resolve();
    const read = async <T,>(name: string, task: () => Promise<T>): Promise<T | undefined> => {
      try { return await task(); }
      catch (error) { log(`${name}: ${errorText(error)}`); return undefined; }
    };
    setChecking(true);
    const work = async () => {
      const [docker, git, wsl, network, space, address, health, reboot] = await Promise.all([
        read("Rancher Desktop check", () => bridge.DockerStatus()),
        read("Git check", () => bridge.GitStatus()),
        platform === "windows" ? read("WSL check", () => bridge.WSLStatus()) : undefined,
        platform === "windows" ? read("network check", () => bridge.NetworkStatus()) : undefined,
        read("space check", () => bridge.DiskStatus()),
        read("address check", () => bridge.MDNSStatus(mdnsName)),
        read("clinic check", () => bridge.ClinicHealth()),
        platform === "windows" ? read("restart check", () => bridge.RestartPlan()) : undefined,
      ]);
      for (const [name, status] of [
        ["Rancher Desktop", docker], ["Git", git], ["WSL 2", wsl],
        ["Network", network], ["Space", space], ["Address", address],
      ] as const) {
        if (status && !status.ok && status.message) log(`${name}: ${status.message}`);
      }
      if (health && !health.active && health.detail) log(`clinic health: ${health.detail}`);
      const [dockerPlan, gitPlan] = await Promise.all([
        docker && !docker.ok ? read("Rancher Desktop plan", () => bridge.DockerPlan()) : undefined,
        git && !git.ok ? read("Git plan", () => bridge.GitPlan()) : undefined,
      ]);
      const unknown = (id: CheckId): PanelCheck => ({
        id, title: TITLES[id], state: "unknown",
        detail: id === "docker" || id === "git" ? SOFTWARE_DESCRIPTIONS[id]
          : "Couldn't check this. Check again, or share the log file with support.",
      });
      const next: PanelCheck[] = [];
      if (platform === "windows" && (!wsl || wsl.applicable)) {
        next.push(!wsl || !reboot ? unknown("wsl") : {
          id: "wsl", title: TITLES.wsl,
          state: wsl.ok && !reboot.needed ? "ready" : "bad",
          detail: reboot.needed ? "Windows needs to restart before CARE can run."
            : wsl.ok ? "The Windows software CARE needs is ready."
              : "WSL 2 needs to be turned on before Rancher Desktop can run.",
          action: reboot.needed ? {
            label: "Restart Windows", run: async () => setRestart(reboot),
          } : !wsl.ok && wsl.fixable ? {
            label: "Turn on WSL 2", run: () => bridge.InstallWSL(),
          } : undefined,
        });
      }
      next.push(!docker ? unknown("docker") : {
        id: "docker", title: TITLES.docker, state: docker.ok ? "ready" : "bad",
        detail: SOFTWARE_DESCRIPTIONS.docker,
        action: docker.ok ? undefined : toolFix("docker", dockerPlan),
        blocked: next.some((check) => check.id === "wsl" && check.state !== "ready")
          ? "Finish the WSL 2 step first." : undefined,
      });
      next.push(!git ? unknown("git") : {
        id: "git", title: TITLES.git, state: git.ok ? "ready" : "bad",
        detail: SOFTWARE_DESCRIPTIONS.git,
        action: git.ok ? undefined : toolFix("git", gitPlan),
      });
      if (platform === "windows" && (!network || network.applicable)) {
        next.push(!network ? unknown("network") : {
          id: "network", title: TITLES.network, state: network.ok ? "ready" : "bad",
          detail: network.ok ? "Other devices can reach this computer."
            : network.fixable ? "Set this network to Private so other devices can reach CARE."
              : "Check the clinic network in Windows settings, then check again.",
          action: !network.ok && network.fixable
            ? { label: "Set this network to Private", run: () => bridge.FixNetwork() } : undefined,
        });
      }
      next.push(!space ? unknown("disk") : {
        id: "disk", title: TITLES.disk, state: space.ok ? "ready" : "bad",
        detail: space.ok ? `${diskSize(space.free)} free on this computer.`
          : "There isn't enough free space for CARE to keep saving data.",
        action: space.ok ? undefined : { label: "View storage", run: async () => setTab("storage") },
      });
      next.push(!address ? unknown("address") : {
        id: "address", title: TITLES.address, state: address.ok ? "ready" : "bad",
        detail: address.ok ? `${mdnsName} on the clinic Wi-Fi.`
          : "The clinic address couldn't be reached as expected. Check the clinic network, then check again.",
      });
      next.push(!health ? unknown("clinic") : {
        id: "clinic", title: TITLES.clinic,
        state: health.active ? "ready" : !docker?.ok ? "waiting" : "bad",
        detail: health.active ? "Serving CARE to your staff."
          : !docker?.ok ? "Waiting for Rancher Desktop."
            : "CARE isn't answering on this computer.",
        action: docker?.ok && !health.active ? {
          label: "Start clinic", run: () => runAction("start"),
        } : undefined,
      });
      if (mounted.current) {
        setChecks(next);
        setCheckedAt(Date.now());
      }
    };
    pending.current = work().finally(() => {
      pending.current = null;
      if (mounted.current) setChecking(false);
    });
    return pending.current;
  }, [platform, mdnsName, log, toolFix, runAction, setTab]);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  useEffect(() => {
    if (!busy) void recheck();
  }, [busy, recheck]);
  useEffect(() => {
    const timer = window.setInterval(() => void recheck(), 60_000);
    return () => window.clearInterval(timer);
  }, [recheck]);
  useEffect(() => onCareEvent("prereq-download-progress", (value: PrereqDownloadProgress) => {
    if (fixing.current) setDownload(value);
  }), []);
  useEffect(() => {
    if (target === "clinic") setFailure(operationError?.action === "start" ? { message: operationError.message } : null);
  }, [operationError, target]);

  const execute = async (check: PanelCheck) => {
    if (executing.current || !check.action || busyRef.current || updateLock.isActive() || restorePending || check.blocked) return;
    executing.current = true;
    fixing.current = true;
    setConfirmation(null);
    setFailure(null);
    setDownload(null);
    setRunning(check.id);
    setTarget(check.id);
    try {
      const result = await check.action.run();
      if (check.id === "clinic" && result === false) {
        setFailure({ message: "CARE couldn't start. Try again, or open the log file for support." });
        return;
      }
      if (typeof result === "string" && result) log(`requirement action: ${result}`);
      if (platform === "windows" && (check.id === "wsl" || check.id === "docker")) {
        const plan = await bridge.RestartPlan();
        if (plan.needed) setRestart(plan);
      }
    } catch (error) {
      log(`fix ${check.id}: ${errorText(error)}`);
      setFailure(downloadProblem(error) ?? { message: "That didn't finish. Try again. If it keeps failing, share the log file with support." });
    } finally {
      executing.current = false;
      fixing.current = false;
      setRunning(null);
      setDownload(null);
      await recheck();
      await refresh().catch((error) => log(`refresh after fix: ${errorText(error)}`));
    }
  };
  const fix = async (check: PanelCheck) => {
    if (!check.action || fixing.current || confirmation || busyRef.current || updateLock.isActive() || checking || restorePending || check.blocked) return;
    fixing.current = true;
    setTarget(check.id);
    if (!check.action.preview) { await execute(check); return; }
    setPreviewing(true);
    setFailure(null);
    try {
      const info = await bridge.RancherDownloadInfo();
      if (updateLock.isActive()) {
        fixing.current = false;
        setFailure({ message: "Finish the CARE Desktop update, then try the fix again." });
        return;
      }
      setConfirmation({ check, info });
    } catch (error) {
      log(`Rancher Desktop download size: ${errorText(error)}`);
      setFailure(downloadProblem(error, true) ?? { message: "Couldn't check the download. Connect to the internet and try again." });
      fixing.current = false;
    } finally {
      setPreviewing(false);
    }
  };
  const dismissConfirmation = () => {
    setConfirmation(null);
    setTarget(null);
    fixing.current = false;
  };

  const active = running ?? (target === "clinic" && busy && busyLabel === "Starting" ? "clinic" : null);
  return {
    checks, checking, checkedAt, recheck, running: active, target, previewing, failure, download, restart,
    confirmation, dismissConfirmation, confirm: () => confirmation && void execute(confirmation.check),
    fix, dismissRestart: () => setRestart(null),
    working: active !== null || previewing || confirmation !== null || restart !== null,
  };
}

type Checks = ReturnType<typeof useChecks>;
const ChecksContext = createContext<Checks | null>(null);

export function PanelRequirementsProvider({ children, onWorkingChange }: {
  children: ReactNode;
  onWorkingChange?: (working: boolean) => void;
}) {
  const value = useChecks();
  const updateLock = usePanelUpdateLock();
  const { busy, restorePending } = useCare();
  useLayoutEffect(() => { onWorkingChange?.(value.working); }, [value.working, onWorkingChange]);
  return <ChecksContext.Provider value={value}>
    {children}
    <AlertDialog open={value.confirmation !== null} onOpenChange={(open) => { if (!open) value.dismissConfirmation(); }}>
      <AlertDialogContent className="panel-dialog">
        <AlertDialogTitle>Download Rancher Desktop?</AlertDialogTitle>
        <AlertDialogDescription className="panel-dialog-description">
          CARE needs Rancher Desktop to run. Check the download size before continuing, especially on a limited internet connection.
        </AlertDialogDescription>
        <div className="panel-notice panel-download-size">
          <Download aria-hidden="true" />
          <div className="panel-grow"><strong>Download size</strong>
            {value.confirmation && value.confirmation.info.size > 0
              ? diskSize(value.confirmation.info.size) : "The size isn't available."}
          </div>
        </div>
        <p className="panel-small">Nothing has downloaded yet. Your computer may ask for permission to install it.</p>
        <AlertDialogFooter className="panel-dialog-foot">
          <AlertDialogCancel onClick={value.dismissConfirmation}>Not now</AlertDialogCancel>
          <AlertDialogAction disabled={busy || updateLock.active || restorePending} onClick={(event) => { event.preventDefault(); value.confirm(); }}>Download and install</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
    <RestartDialog plan={value.restart} onDismiss={value.dismissRestart} />
  </ChecksContext.Provider>;
}

export function usePanelRequirements() {
  const context = useContext(ChecksContext);
  if (!context) throw new Error("Panel requirements must be inside their provider.");
  return context;
}

export function RequirementActions({ check }: { check: PanelCheck }) {
  const { busy, restorePending } = useCare();
  const requirements = usePanelRequirements();
  const updateLock = usePanelUpdateLock();
  return <>
    {check.blocked ? <p className="panel-small">{check.blocked}</p> : check.action ? <Button variant="primary"
      disabled={busy || updateLock.active || restorePending || requirements.working || requirements.checking}
      onClick={() => void requirements.fix(check)}>
      {requirements.running === check.id ? <Spinner /> : null}
      {requirements.running === check.id ? "Working…" : check.action.label}
    </Button> : check.state === "bad" || check.state === "unknown" ? <PanelLogButton /> : null}
  </>;
}

export function RequirementProgress({ checkId }: { checkId?: CheckId } = {}) {
  const { checks, running, target, previewing, download, failure } = usePanelRequirements();
  if (checkId && target !== checkId) return null;
  const progress = checks.find((check) => check.id === running)?.action?.progress;
  const percent = download?.phase === "downloading" && download.total > 0
    ? Math.max(0, Math.min(100, Math.round(download.done / download.total * 100))) : undefined;
  const label = previewing ? "Checking the download size…"
    : download?.phase === "connecting" ? "Connecting to download…"
      : download?.phase === "downloading" ? "Downloading…"
        : download?.phase === "verifying" ? "Checking the downloaded file…"
          : download?.phase === "complete" ? "Installing — keep CARE Desktop open."
            : download?.phase === "failed" ? "The download didn't finish."
              : progress ?? "Working — keep CARE Desktop open.";
  return <>
    {running || previewing ? <div className="panel-fix-progress" role="status">
      <div className="panel-row"><Spinner /><span>{label}</span></div>
      {download ? <>
        <Progress value={percent} aria-label="Required software download" />
        {download.phase === "downloading" ? <div className="panel-small">
          {download.total > 0 ? `${diskSize(download.done)} of ${diskSize(download.total)}` : `${diskSize(download.done)} downloaded`}
          {percent !== undefined ? ` · ${percent}%` : ""}
        </div> : null}
      </> : null}
    </div> : null}
    {failure ? <PanelNotice title={failure.title ?? "Couldn't finish the fix"} actions={<PanelLogButton />}>{failure.message}</PanelNotice> : null}
  </>;
}

export function RequirementsCard() {
  const { busy } = useCare();
  const requirements = usePanelRequirements();
  const core = requirements.checks.filter((check) => CORE.has(check.id));
  const failed = core.filter((check) => check.state === "bad" || check.state === "unknown");
  if (!failed.length && !requirements.failure) return null;
  return <section className="panel-card panel-card-pad panel-requirements" aria-label="What the clinic needs">
    <div className="panel-row panel-between">
      <div>
        <h2 className="panel-eyebrow">What the clinic needs</h2>
        <p className="panel-card-sub">{requirements.checkedAt
          ? `Checked at ${new Date(requirements.checkedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
          : "Checking this computer…"}</p>
      </div>
      <Button size="sm" disabled={busy || requirements.checking || requirements.working} onClick={() => void requirements.recheck()}>
        {requirements.checking ? <Spinner /> : <RefreshCw aria-hidden="true" />}
        {requirements.checking ? "Checking…" : "Check now"}
      </Button>
    </div>
    <div className="panel-badges">
      {core.map((check) => <PanelBadge key={check.id} tone={check.state === "ready" ? "ok" : "danger"}>
        {check.title}{check.state === "ready" ? " available" : check.state === "unknown" ? " couldn't be checked" : " needs setup"}
      </PanelBadge>)}
    </div>
    {failed.map((check) => <div className="panel-requirement-fix" key={check.id}>
      <p>{check.detail}</p><RequirementActions check={check} />
    </div>)}
    <RequirementProgress />
  </section>;
}

export function RequirementChecklist() {
  const { checks, checking, target, running, previewing, failure } = usePanelRequirements();
  const activeWork = useRef<HTMLDivElement>(null);
  useEffect(() => {
    activeWork.current?.scrollIntoView({ block: "nearest" });
  }, [target, running, previewing, failure]);
  if (!checks.length) return <div className="panel-fix-progress" role="status"><Spinner /> Checking this computer…</div>;
  return <div className="panel-checklist" aria-busy={checking}>
    {checks.map((check) => {
      const failed = check.state === "bad" || check.state === "unknown";
      const tone = check.state === "ready" ? "ok" : failed ? "danger" : "neutral";
      return <div className={`panel-check ${failed ? "panel-check-failed" : ""}`} key={check.id}>
        <div className="panel-check-head">
          <span className={`panel-check-dot panel-tone-${tone}`}>
            {check.state === "ready" ? <Check aria-hidden="true" /> : failed ? <X aria-hidden="true" />
              : <Spinner />}
          </span>
          <div className="panel-grow"><h3 className="panel-title">{check.title}</h3><p className="panel-card-sub">{check.detail}</p></div>
          <PanelBadge tone={tone}>{check.state === "ready" ? "Available" : check.state === "bad" ? "Needs setup" : check.state === "unknown" ? "Couldn't check" : "Waiting"}</PanelBadge>
        </div>
        {failed ? <div className="panel-check-fix"><RequirementActions check={check} /></div> : null}
        {target === check.id && (running || previewing || failure) ? <div className="panel-check-work" ref={activeWork}>
          <RequirementProgress checkId={check.id} />
        </div> : null}
      </div>;
    })}
  </div>;
}
