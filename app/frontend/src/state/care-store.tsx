// One store for everything that outlives a single screen: which view is up, how
// the install is progressing, and the panel's view of the server. Screens own
// their own form state; this owns the parts the rail and the Wails events touch.
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { toast } from "@/components/ui/sonner";
import { bridge, logToHost, onCareEvent } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { operationError as describeOperationError, type OperationError } from "@/lib/operation-errors";
import { RUN_STEPS, type RunStep } from "@/lib/run-steps";
import type { AppUpdateProgress, Backup, CareUpdate, SetupFailure, SetupPage, StorageReport } from "@/types";

export type Flow = "role" | "client" | "setup" | "installing" | "failed" | "panel" | "remove";
export type SetupStep = "checks" | "backup" | "admin" | "install";
export type PanelTab = "overview" | "storage" | "backups" | "plugins" | "updates" | "advanced";
export type SystemState = "running" | "partial" | "stopped" | "unknown";

export type InstallParams = {
  host: string;
  adminPassword: string;
  backupDir: string;
  pages?: SetupPage[];
};

export type RunState = {
  steps: RunStep[];
  stepIdx: number;
  pct: number;
  startedAt: number;
  finished: boolean;
  failMessage: string;
  failure: SetupFailure | null;
  pages: SetupPage[];
};

const IDLE_RUN: RunState = {
  steps: RUN_STEPS,
  stepIdx: 0,
  pct: 0,
  startedAt: 0,
  finished: false,
  failMessage: "",
  failure: null,
  pages: ["space", "software", "address", "backup", "admin", "review", "install"],
};

const ACTION_LABELS: Record<string, string> = {
  start: "Starting",
  stop: "Stopping",
  restart: "Restarting",
  "rebuild-all": "Rebuilding",
  "rebuild-frontend": "Rebuilding",
  "rebuild-backend": "Rebuilding",
  "apply-plugins": "Applying plugins",
  "backup-now": "Backing up",
  update: "Updating CARE",
  "free-space": "Freeing space",
};

export const RESTORE_PENDING_NOTICE =
  "An earlier restore is unfinished. Start CARE to recover it before doing anything else.";

// Rancher Desktop needs about a minute after a reboot before it can answer, and
// the panel starts the clinic itself on launch. Nothing is wrong until then.
const GRACE_MS = 90_000;

const NO_STEPS_DONE: Record<SetupStep, boolean> = {
  checks: false,
  backup: false,
  admin: false,
  install: false,
};

type CareStore = {
  ready: boolean;
  flow: Flow;
  mdnsName: string;
  clientURL: string;
  selectRole: (role: "server" | "client") => void;
  clearRole: () => Promise<boolean>;

  /** Which setup section is expanded — the rail highlights the same one. */
  openStep: SetupStep;
  setOpenStep: (step: SetupStep) => void;
  stepsDone: Record<SetupStep, boolean>;
  setStepDone: (step: SetupStep, done: boolean) => void;

  run: RunState;
  setupReset: number;
  startInstall: (params: InstallParams) => Promise<void>;
  resumeInstall: () => Promise<void>;
  retryInstall: () => Promise<void>;
  restartSetup: () => void;
  openPanel: () => void;

  tab: PanelTab;
  setTab: (tab: PanelTab) => void;
  busy: boolean;
  busyLabel: string;
  operationError: OperationError | null;
  clearOperationError: () => void;
  system: SystemState;
  systemDetail: string;
  /** The clinic is down and nobody asked for that - the panel says so. */
  trouble: boolean;
  careUpdate: CareUpdate | null;
  applyCareUpdate: () => Promise<void>;
  dismissCareUpdate: () => Promise<void>;
  installAppUpdate: () => Promise<void>;
  acknowledgeAppUpdate: () => boolean;
  restorePending: boolean;
  version: string;
  platform: string;
  backups: Backup[];
  backupsError: string;
  autostart: boolean;
  autostartReady: boolean;
  autostartSaving: boolean;
  autostartError: string;
  recheckAutostart: () => Promise<boolean>;
  storage: StorageReport | null;
  storageError: string;
  recheckStorage: () => Promise<void>;
  refresh: () => Promise<void>;
  reloadBackups: () => Promise<void>;
  /** Resolves true when the native job is accepted; care-done reports completion. */
  runAction: (action: string, adminPassword?: string) => Promise<boolean>;
  setAutostart: (on: boolean) => Promise<void>;
  restore: (backup: Backup, recoveryFile: string, adminPassword: string) => Promise<void>;
  restoreFile: (path: string, recoveryFile: string, adminPassword: string) => Promise<void>;
  uninstall: (
    removeImages: boolean,
    removeBackups: boolean,
    removeRancher: boolean,
    adminPassword: string,
    removeApp?: boolean,
  ) => Promise<boolean>;
  log: (line: string) => void;
};

const CareContext = createContext<CareStore | null>(null);

export function useCare(): CareStore {
  const store = useContext(CareContext);
  if (!store) throw new Error("useCare must be used inside <CareProvider>");
  return store;
}

export function CareProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  const [flow, setFlowState] = useState<Flow>("role");
  const [clientURL, setClientURL] = useState("");
  const [mdnsName, setMdnsName] = useState("care.local");
  const [openStep, setOpenStep] = useState<SetupStep>("checks");
  const [stepsDone, setStepsDone] = useState(NO_STEPS_DONE);
  const [run, setRunState] = useState<RunState>(IDLE_RUN);
  const [setupReset, setSetupReset] = useState(0);
  const [tab, setTab] = useState<PanelTab>("overview");
  const [busy, setBusyState] = useState(false);
  const [busyLabel, setBusyLabel] = useState("");
  const [operationError, setOperationError] = useState<OperationError | null>(null);
  const [system, setSystem] = useState<SystemState>("unknown");
  const [systemDetail, setSystemDetail] = useState("");
  const [restorePending, setRestorePending] = useState(false);
  const [version, setVersion] = useState("");
  const [platform, setPlatform] = useState("");
  const [backups, setBackups] = useState<Backup[]>([]);
  const [backupsError, setBackupsError] = useState("");
  const [autostart, setAutostartState] = useState(false);
  const [autostartReady, setAutostartReady] = useState(false);
  const [autostartSaving, setAutostartSaving] = useState(false);
  const [autostartError, setAutostartError] = useState("");
  const [trouble, setTrouble] = useState(false);
  const [careUpdate, setCareUpdate] = useState<CareUpdate | null>(null);
  const [storage, setStorage] = useState<StorageReport | null>(null);
  const [storageError, setStorageError] = useState("");
  const [bootError, setBootError] = useState<Error | null>(null);

  // Refs shadow the state the event handlers and the poll timer read, so they
  // never work from a stale closure and never need to re-subscribe.
  const flowRef = useRef<Flow>("role");
  const runRef = useRef<RunState>(IDLE_RUN);
  const busyRef = useRef(false);
  const restorePendingRef = useRef(false);
  // Buffered so the fail screen can show the real build error rather than just
  // "exit status 1". Kept out of React state: the install emits thousands of
  // lines and only a step change needs to repaint.
  const logRef = useRef<string[]>([]);
  const lastErrorRef = useRef("");
  const removeAppRef = useRef(false);
  const uninstalledRef = useRef(false);
  const uninstallEndedRef = useRef(false);
  const activeActionRef = useRef("");
  const refreshPendingRef = useRef(false);
  const refreshEpochRef = useRef(0);
  const autostartPendingRef = useRef(false);
  const setupEndedRef = useRef(false);
  const setupFailureReceivedRef = useRef(false);
  const bootStartedRef = useRef(false);
  const stopIntentBeforeRef = useRef(false);
  const stateRefreshNeededRef = useRef(false);
  const appUpdatePhaseRef = useRef<AppUpdateProgress["phase"] | null>(null);
  const appUpdateHandoffRef = useRef(false);

  // Three things stand between "health check failed" and alarming the operator.
  // Stopping the clinic is a legitimate thing to do, Docker takes about a minute
  // to come up after a reboot, and a container restarting shouldn't raise an
  // alarm that is still on screen after it recovers.
  const stoppedOnPurposeRef = useRef(false);
  const panelSinceRef = useRef(0);
  const downStreakRef = useRef(0);

  const setFlow = useCallback((next: Flow) => {
    flowRef.current = next;
    setFlowState(next);
  }, []);

  const selectRole = useCallback((role: "server" | "client") => {
    if (busyRef.current || flowRef.current !== "role") return;
    setFlow(role === "client" ? "client" : "setup");
  }, [setFlow]);

  const setRun = useCallback((next: RunState) => {
    runRef.current = next;
    setRunState(next);
  }, []);

  const setBusy = useCallback((next: boolean, label = "") => {
    if (next) refreshEpochRef.current++;
    busyRef.current = next;
    setBusyState(next);
    setBusyLabel(label);
  }, []);

  const clearOperationError = useCallback(() => setOperationError(null), []);

  const setStepDone = useCallback((step: SetupStep, done: boolean) => {
    setStepsDone((prev) => (prev[step] === done ? prev : { ...prev, [step]: done }));
  }, []);

  const pushLine = useCallback(
    (line: string) => {
      logRef.current.push(line);
      if (logRef.current.length > 300) logRef.current.shift();
      const current = runRef.current;
      for (let i = 0; i < current.steps.length; i++) {
        if (!current.steps[i].re.test(line)) continue;
        if (current.steps[i].pct > current.pct) {
          setRun({ ...current, stepIdx: i, pct: current.steps[i].pct });
        }
        return;
      }
    },
    [setRun],
  );

  // Lines raised HERE (a failed save, a caught render error) have never been near
  // Go, so they reach the log file only if we send them. Lines arriving on
  // care-log are already in it — see logFromHost below.
  const log = useCallback((line: string) => {
    logToHost(line);
    if (flowRef.current === "panel") {
      return;
    }
    pushLine(line);
  }, [pushLine]);

  // Same buffer and progress-bar handling as log(), minus the write back to the
  // host: Go already put this line in the file.
  const logFromHost = useCallback(
    (line: string) => {
      if (flowRef.current === "panel") return;
      pushLine(line);
    },
    [pushLine],
  );

  const clearRole = useCallback(async () => {
    try {
      await bridge.ClearRole();
    } catch (e) {
      log(`role: ${errorText(e)}`);
      toast(/already set up|uninstall/i.test(errorText(e))
        ? "This computer already has a saved setup. Disconnect or remove it before going back."
        : "Couldn't go back. Try again, or share the log file with your support contact.");
      return false;
    }
    setClientURL("");
    setFlow("role");
    return true;
  }, [log, setFlow]);

  const failInstall = useCallback(() => {
    const tail = logRef.current.slice(-40).join("\n").trim();
    const headline = (lastErrorRef.current || "Setup did not complete.").trim();
    setRun({
      ...runRef.current,
      failMessage: tail ? `${headline}\n\n---- last output ----\n${tail}` : headline,
    });
    setFlow("failed");
  }, [setFlow, setRun]);

  // --- panel ------------------------------------------------------------
  const refresh = useCallback(async () => {
    if (busyRef.current || flowRef.current !== "panel" || refreshPendingRef.current) return;
    refreshPendingRef.current = true;
    const epoch = refreshEpochRef.current;
    let next: SystemState;
    let detail = "";
    try {
      if (stateRefreshNeededRef.current) {
        const state = await bridge.GetState();
        restorePendingRef.current = state.restore_pending;
        setRestorePending(state.restore_pending);
        stateRefreshNeededRef.current = false;
      }
      const health = await bridge.ClinicHealth();
      if (health.active) next = "running";
      else {
        const ps = await bridge.ClinicStatus();
        next = ps.trim() ? "partial" : "stopped";
      }
    } catch (e) {
      next = "unknown";
      detail = "CARE couldn't check the clinic. Check the required software or open the log file for support.";
      log(`clinic status: ${errorText(e)}`);
    } finally {
      refreshPendingRef.current = false;
    }
    if (epoch !== refreshEpochRef.current || flowRef.current !== "panel") return;
    setSystem(next);
    setSystemDetail(detail);

    downStreakRef.current = next === "running" ? 0 : downStreakRef.current + 1;
    const settled = Date.now() - panelSinceRef.current > GRACE_MS;
    setTrouble(
      downStreakRef.current >= 2 && settled && !stoppedOnPurposeRef.current && !busyRef.current,
    );
  }, [log]);

  const reloadBackups = useCallback(async () => {
    try {
      setBackups((await bridge.ListBackups()) ?? []);
      setBackupsError("");
    } catch (e) {
      setBackupsError("The backup list couldn't be read. Check the backup location and try again.");
      log(`backups: ${errorText(e)}`);
    }
  }, [log]);

  const recheckStorage = useCallback(async () => {
    try {
      setStorage(await bridge.RecheckStorage());
      setStorageError("");
    } catch (e) {
      log(`storage: ${errorText(e)}`);
      setStorageError("Storage couldn't be checked. The last reading may be out of date. Try again.");
    }
  }, [log]);

  const runAction = useCallback(
    async (action: string, adminPassword = "") => {
      if (busyRef.current) {
        setOperationError(describeOperationError(action, "something else is still running"));
        return false;
      }
      if (stateRefreshNeededRef.current && action !== "start" && action !== "stop") {
        setOperationError({ action, title: "Check the clinic's status first", message: "CARE couldn't check whether a restore needs recovery. Check again before making changes." });
        return false;
      }
      if (restorePendingRef.current && action !== "start" && action !== "stop") {
        setOperationError(describeOperationError(action, "a restore is unfinished"));
        return false;
      }
      // What the operator asked for, which is what makes a stopped clinic either
      // a fault or a choice. Not persisted: the panel starts the clinic on every
      // launch, so the intent dies with the session, same as the state it describes.
      stopIntentBeforeRef.current = stoppedOnPurposeRef.current;
      if (action === "stop") stoppedOnPurposeRef.current = true;
      if (action === "start" || action === "restart") {
        stoppedOnPurposeRef.current = false;
        // Down and being fixed is not down and unattended. refresh() skips while
        // busy, so without this the banner would sit there through the restart.
        downStreakRef.current = 0;
        setTrouble(false);
      }
      setBusy(true, ACTION_LABELS[action] ?? "Working");
      activeActionRef.current = action;
      lastErrorRef.current = "";
      setOperationError(null);
      log(`\n$ care ${action}`);
      try {
        await bridge.ClinicAction(action, adminPassword);
        return true;
      } catch (e) {
        log(`error: ${errorText(e)}`);
        setOperationError(describeOperationError(action, e));
        if (action === "stop") stoppedOnPurposeRef.current = stopIntentBeforeRef.current;
        activeActionRef.current = "";
        setBusy(false);
        return false;
      }
    },
    [log, setBusy],
  );

  const applyCareUpdate = useCallback(async () => {
    await runAction("update");
  }, [runAction]);

  const installAppUpdate = useCallback(async () => {
    if (busyRef.current) throw new Error("Something else is still running.");
    appUpdatePhaseRef.current = null;
    appUpdateHandoffRef.current = false;
    lastErrorRef.current = "";
    setBusy(true, "Updating CARE Desktop");
    activeActionRef.current = "app-update";
    log("\n$ care-desktop update");
    try {
      await bridge.InstallAppUpdate();
    } catch (e) {
      log(`error: ${errorText(e)}`);
      if (flowRef.current !== "role") {
        toast.error("CARE Desktop couldn't finish updating. Your current version was kept. Try again.");
      }
      setBusy(false);
      activeActionRef.current = "";
      throw e;
    }
  }, [log, setBusy]);

  const acknowledgeAppUpdate = useCallback(() => {
    if (!appUpdateHandoffRef.current || appUpdatePhaseRef.current !== "installer") {
      return !busyRef.current;
    }
    appUpdateHandoffRef.current = false;
    appUpdatePhaseRef.current = null;
    activeActionRef.current = "";
    setBusy(false);
    return true;
  }, [setBusy]);

  const dismissCareUpdate = useCallback(async () => {
    if (busyRef.current) return;
    try {
      await bridge.DismissCareUpdate();
      setCareUpdate(null);
    } catch (e) {
      log(`update: ${errorText(e)}`);
      setOperationError(describeOperationError("dismiss-update", e));
    }
  }, [log]);

  const syncAutostart = useCallback(async () => {
    try {
      setAutostartState(await bridge.AutostartEnabled());
      setAutostartReady(true);
      setAutostartError("");
      return true;
    } catch (e) {
      log(`read startup setting: ${errorText(e)}`);
      setAutostartReady(false);
      setAutostartError("The startup setting couldn't be checked. Try again.");
      return false;
    }
  }, [log]);

  const setAutostart = useCallback(
    async (on: boolean) => {
      if (busyRef.current || autostartPendingRef.current) return;
      autostartPendingRef.current = true;
      setAutostartSaving(true);
      setAutostartError("");
      try {
        await bridge.SetAutostart(on);
        if (await syncAutostart()) toast(on ? "Start at login on" : "Start at login off");
      } catch (e) {
        log(`autostart error: ${errorText(e)}`);
        setAutostartError("The startup setting couldn't be saved. Try again.");
      } finally {
        autostartPendingRef.current = false;
        setAutostartSaving(false);
      }
    },
    [log, syncAutostart],
  );

  const restore = useCallback(
    async (backup: Backup, recoveryFile: string, adminPassword: string) => {
      if (busyRef.current) throw new Error("something else is still running");
      if (restorePendingRef.current) {
        throw new Error("a restore is unfinished; start CARE to recover it before making other changes");
      }
      if (stateRefreshNeededRef.current) throw new Error("CARE couldn't check the restore status. Check the clinic again.");
      setBusy(true, "Restoring");
      activeActionRef.current = "restore";
      lastErrorRef.current = "";
      setOperationError(null);
      log(
        `\n$ care restore ${backup.db_dump}${backup.files_archive ? ` ${backup.files_archive}` : ""}`,
      );
      try {
        await bridge.RestoreBackup(backup.db_dump, backup.files_archive, recoveryFile, adminPassword);
      } catch (e) {
        log(`error: ${errorText(e)}`);
        setBusy(false);
        activeActionRef.current = "";
        setOperationError(describeOperationError("restore", e));
        throw e;
      }
    },
    [log, setBusy],
  );

  const restoreFile = useCallback(
    async (path: string, recoveryFile: string, adminPassword: string) => {
      if (busyRef.current) throw new Error("something else is still running");
      if (restorePendingRef.current) {
        throw new Error("a restore is unfinished; start CARE to recover it before making other changes");
      }
      if (stateRefreshNeededRef.current) throw new Error("CARE couldn't check the restore status. Check the clinic again.");
      setBusy(true, "Restoring");
      activeActionRef.current = "restore";
      lastErrorRef.current = "";
      setOperationError(null);
      log("\n$ care restore imported backup");
      try {
        await bridge.RestoreFromFile(path, recoveryFile, adminPassword);
      } catch (e) {
        log(`error: ${errorText(e)}`);
        setBusy(false);
        activeActionRef.current = "";
        setOperationError(describeOperationError("restore", e));
        throw e;
      }
    },
    [log, setBusy],
  );

  const uninstall = useCallback(
    async (
      removeImages: boolean,
      removeBackups: boolean,
      removeRancher: boolean,
      adminPassword: string,
      removeApp = false,
    ) => {
      if (busyRef.current) {
        setOperationError(describeOperationError("uninstall", "something else is still running"));
        return false;
      }
      removeAppRef.current = removeApp;
      uninstalledRef.current = false;
      uninstallEndedRef.current = false;
      setBusy(true, "Uninstalling");
      activeActionRef.current = "uninstall";
      lastErrorRef.current = "";
      setOperationError(null);
      log(
        `\n$ care uninstall${removeImages ? " --images" : ""}${removeBackups ? " --backups" : ""}${removeRancher ? " --rancher" : ""} --yes`,
      );
      try {
        await bridge.RunUninstall(removeImages, removeBackups, removeRancher, adminPassword);
        return true;
      } catch (e) {
        log(`error: ${errorText(e)}`);
        setBusy(false);
        activeActionRef.current = "";
        setOperationError(describeOperationError("uninstall", e));
        removeAppRef.current = false;
        return false;
      }
    },
    [log, setBusy],
  );

  const bootPanel = useCallback(async () => {
    panelSinceRef.current = Date.now();
    stoppedOnPurposeRef.current = false;
    downStreakRef.current = 0;
    setTrouble(false);
    let restorePending = false;
    try {
      const state = await bridge.GetState();
      if (state.role !== "server") {
        setFlow(state.role === "client" ? "client" : "role");
        return;
      }
      setMdnsName(state.mdns_name || "care.local");
      restorePending = state.restore_pending;
      restorePendingRef.current = restorePending;
      setRestorePending(restorePending);
    } catch (e) {
      setBootError(new Error(errorText(e)));
      return;
    }
    setTab("overview");
    void bridge.StorageStatus().then((report) => { setStorage(report); setStorageError(""); }, (e) => {
      log(`storage: ${errorText(e)}`);
      setStorageError("Storage couldn't be checked. Try again.");
    });
    await reloadBackups();
    await refresh();
    await syncAutostart();
    try {
      const health = await bridge.ClinicHealth();
      if ((!health.active || restorePending) && !busyRef.current) {
        log(
          restorePending
            ? "\nRecovering an unfinished restore..."
            : (await bridge.WasAutostartLaunched())
              ? "\nLaunched at startup — starting CARE..."
              : "\nCARE isn't running — starting it...",
        );
        await runAction("start");
      }
    } catch (e) {
      log(`startup status: ${errorText(e)}`);
      setSystemDetail("CARE couldn't check whether the clinic is running. Check again or open the log file for support.");
    }
  }, [log, refresh, reloadBackups, runAction, syncAutostart, setFlow]);

  const openPanel = useCallback(() => {
    setFlow("panel");
    void bootPanel();
  }, [bootPanel, setFlow]);

  // --- setup flow -------------------------------------------------------
  const startInstall = useCallback(
    async (params: InstallParams) => {
      if (busyRef.current || flowRef.current !== "setup") {
        throw new Error("Something else is still running. Wait for it to finish.");
      }
      setBusy(true, "Checking setup");
      activeActionRef.current = "setup";
      setupEndedRef.current = false;
      setupFailureReceivedRef.current = false;
      logRef.current = [];
      lastErrorRef.current = "";
      setMdnsName(params.host);
      setRun({
        steps: RUN_STEPS,
        stepIdx: 0,
        pct: 0,
        startedAt: Date.now(),
        finished: false,
        failMessage: "",
        failure: null,
        pages: params.pages ?? IDLE_RUN.pages,
      });
      try {
        await bridge.RunSetup(
          params.host,
          params.adminPassword,
          params.backupDir,
        );
        if (flowRef.current === "setup") {
          setFlow("installing");
          setBusy(true, "Installing CARE");
        }
        log("Starting one-time setup...");
      } catch (e) {
        log(`setup validation: ${errorText(e)}`);
        if (activeActionRef.current === "setup") {
          activeActionRef.current = "";
          setBusy(false);
        }
        throw e;
      }
    },
    [log, setBusy, setFlow, setRun],
  );

  const resumeInstall = useCallback(async () => {
    if (busyRef.current || flowRef.current !== "failed" || !runRef.current.failure?.can_retry) {
      throw new Error("This installation isn't ready to retry.");
    }
    const previous = runRef.current;
    setBusy(true, "Retrying installation");
    activeActionRef.current = "setup";
    setupEndedRef.current = false;
    setupFailureReceivedRef.current = false;
    logRef.current = [];
    lastErrorRef.current = "";
    setRun({ ...IDLE_RUN, pages: previous.pages, failure: previous.failure, startedAt: Date.now() });
    try {
      await bridge.RetrySetup();
      if (!setupEndedRef.current || activeActionRef.current === "setup") {
        if (!setupFailureReceivedRef.current) setRun({ ...runRef.current, failure: null });
        setFlow("installing");
        setBusy(true, "Installing CARE");
      }
      log("Retrying the unfinished installation...");
    } catch (error) {
      log(`retry installation: ${errorText(error)}`);
      if (!setupEndedRef.current) {
        setRun(previous);
        activeActionRef.current = "";
        setBusy(false);
      }
      throw error;
    }
  }, [log, setBusy, setFlow, setRun]);

  const restartSetup = useCallback(() => {
    lastErrorRef.current = "";
    logRef.current = [];
    setRun(IDLE_RUN);
    setOpenStep("checks");
    setStepsDone(NO_STEPS_DONE);
    setFlow("setup");
  }, [setFlow, setRun]);

  const retryInstall = useCallback(async () => {
    if (busyRef.current) throw new Error("something else is still running");
    setBusy(true, "Clearing the unfinished installation");
    activeActionRef.current = "cleanup-failed";
    try {
      await bridge.CleanupFailedInstall();
      setSetupReset((value) => value + 1);
      restartSetup();
    } catch (e) {
      log(`cleanup: ${errorText(e)}`);
      throw e;
    } finally {
      activeActionRef.current = "";
      setBusy(false);
    }
  }, [log, restartSetup, setBusy]);

  const finishUninstall = useCallback(() => {
    if (!uninstalledRef.current || !uninstallEndedRef.current) return;
    uninstalledRef.current = false;
    uninstallEndedRef.current = false;
    activeActionRef.current = "";
    if (flowRef.current === "remove") {
      void bridge.ExitUninstall().catch((e) => {
        log(`finish uninstall: ${errorText(e)}`);
        setBusy(false);
        toast.error("The clinic was removed, but the uninstaller couldn't close. Close this window to finish.");
      });
      return;
    }
    const removeApp = removeAppRef.current;
    removeAppRef.current = false;
    setMdnsName("care.local");
    setClientURL("");
    setBackups([]);
    setBackupsError("");
    setStorage(null);
    setStorageError("");
    setCareUpdate(null);
    setOperationError(null);
    restorePendingRef.current = false;
    stateRefreshNeededRef.current = false;
    setRestorePending(false);
    setStepsDone(NO_STEPS_DONE);
    setRun(IDLE_RUN);
    setSetupReset((value) => value + 1);
    setFlow("role");
    setBusy(removeApp, removeApp ? "Removing CARE Desktop" : "");
    if (removeApp) {
      void bridge.RemoveApp().catch((e) => {
        log(`remove app: ${errorText(e)}`);
        setBusy(false);
        toast.error("The clinic was removed, but CARE Desktop couldn't remove itself. Close it and remove the application using this computer's settings.");
      });
    } else {
      toast("The clinic was removed. Backups you chose to keep are still in their folder.");
    }
  }, [log, setBusy, setFlow, setRun]);

  // --- host events ------------------------------------------------------
  useEffect(() => {
    const unsubscribes = [
      onCareEvent("care-log", (line: string) => {
        if (line.startsWith("error:")) {
          lastErrorRef.current = line.slice("error:".length).trim();
        }
        logFromHost(line);
      }),
      onCareEvent("care-error", (title: string, detail: string) => {
        if (title === "The CARE Desktop update didn't finish") return;
        lastErrorRef.current = detail;
        if (flowRef.current === "panel") {
          const error = describeOperationError(activeActionRef.current || "operation", detail);
          setOperationError({ ...error, title });
        }
      }),
      onCareEvent("setup-failed", (failure: SetupFailure) => {
        if (activeActionRef.current !== "setup" || setupEndedRef.current) return;
        setupFailureReceivedRef.current = true;
        setRun({ ...runRef.current, failure });
      }),
      onCareEvent("app-update-progress", (progress: AppUpdateProgress) => {
        if (activeActionRef.current && activeActionRef.current !== "app-update") return;
        appUpdatePhaseRef.current = progress.phase;
        // A reloaded UI may attach while the native updater is already running.
        if (!activeActionRef.current) {
          activeActionRef.current = "app-update";
          setBusy(true, "Updating CARE Desktop");
        }
      }),
      onCareEvent("care-done", (code: number, label?: string) => {
        if (label === "app-update") {
          if (activeActionRef.current && activeActionRef.current !== label) return;
          if (code === 0 && (appUpdatePhaseRef.current === "installer" || appUpdatePhaseRef.current === "restarting")) {
            appUpdateHandoffRef.current = true;
            activeActionRef.current = "app-update";
            setBusy(true, "Finish the CARE Desktop update");
            return;
          }
          appUpdatePhaseRef.current = null;
          appUpdateHandoffRef.current = false;
          activeActionRef.current = "";
          setBusy(false);
          if (code !== 0 && flowRef.current !== "role") {
            toast.error("CARE Desktop couldn't finish updating. Your current version was kept. Try again.");
          }
          return;
        }
        if (label === "uninstall" && (flowRef.current === "panel" || flowRef.current === "remove")) {
          if (code === 0) {
            uninstallEndedRef.current = true;
            finishUninstall();
          } else {
            uninstalledRef.current = false;
            uninstallEndedRef.current = false;
            removeAppRef.current = false;
            activeActionRef.current = "";
            setBusy(false);
            const error = describeOperationError("uninstall", lastErrorRef.current);
            setOperationError(error);
            if (flowRef.current === "remove") toast.error(error.title);
            stateRefreshNeededRef.current = true;
            void refresh();
          }
          return;
        }
        if (flowRef.current === "role" || flowRef.current === "client") return;
        if (flowRef.current === "remove") {
          setBusy(false);
          if (code !== 0) {
            toast.error(describeOperationError("uninstall", lastErrorRef.current).message);
          }
          return;
        }
        if (flowRef.current !== "panel") {
          if (label === "setup" && activeActionRef.current === "setup") {
            setupEndedRef.current = true;
            if (code !== 0) {
              activeActionRef.current = "";
              setBusy(false);
              log(`\n× Setup failed (exit ${code}).`);
              failInstall();
            } else if (runRef.current.finished) {
              activeActionRef.current = "";
              setBusy(false);
              openPanel();
            }
          }
          return;
        }
        if (activeActionRef.current && label !== activeActionRef.current) return;
        log(`— done (exit ${code}) —`);
        if (code !== 0) {
          setOperationError(describeOperationError(label ?? "operation", lastErrorRef.current));
          if (label === "stop") stoppedOnPurposeRef.current = stopIntentBeforeRef.current;
        } else if (label === "update") {
          setCareUpdate(null);
        }
        activeActionRef.current = "";
        setBusy(false);
        void refresh();
        void reloadBackups();
        void recheckStorage();
        void bridge.GetState().then((state) => {
          stateRefreshNeededRef.current = false;
          restorePendingRef.current = state.restore_pending;
          setRestorePending(state.restore_pending);
          if (!state.setup_done) {
            setStepsDone(NO_STEPS_DONE);
            setOpenStep("checks");
            setFlow("setup");
          }
        }).catch((e) => {
          stateRefreshNeededRef.current = true;
          log(`state: ${errorText(e)}`);
          setOperationError({ action: "status", title: "The clinic's status couldn't be refreshed", message: "CARE couldn't check whether a restore needs recovery. Check again before making changes." });
        });
      }),
      onCareEvent("setup-done", () => {
        if (activeActionRef.current !== "setup") return;
        const current = runRef.current;
        setRun({
          ...current,
          pct: 100,
          stepIdx: current.steps.length - 1,
          finished: true,
        });
        setStepDone("install", true);
        if (setupEndedRef.current) {
          activeActionRef.current = "";
          setBusy(false);
          openPanel();
        }
      }),
      onCareEvent("care-update", (update: CareUpdate) => {
        setCareUpdate(update);
      }),
      onCareEvent("care-storage", (report: StorageReport) => {
        setStorage(report);
        setStorageError("");
      }),
      onCareEvent("uninstalled", () => {
        if (flowRef.current !== "panel" && flowRef.current !== "remove") return;
        uninstalledRef.current = true;
        finishUninstall();
      }),
    ];
    return () => unsubscribes.forEach((off) => off?.());
  }, [
    failInstall,
    log,
    recheckStorage,
    refresh,
    reloadBackups,
    setBusy,
    setRun,
    setStepDone,
    setFlow,
    openPanel,
    finishUninstall,
  ]);

  // --- boot + status poll ----------------------------------------------
  useEffect(() => {
    if (bootStartedRef.current) return;
    bootStartedRef.current = true;
    void (async () => {
      try {
        const state = await bridge.GetState();
        setVersion(state.version);
        setPlatform(state.platform);
        setMdnsName(state.mdns_name || "care.local");
        setClientURL(state.client_url || "");
        if (await bridge.UninstallRequested()) {
          setFlow("remove");
          return;
        }
        if (state.role === "client") {
          setFlow("client");
        } else if (state.role === "server" && state.setup_done) {
          setFlow("panel");
          await bootPanel();
        } else if (state.role === "server") {
          setFlow("setup");
        }
      } catch (e) {
        setBootError(new Error(errorText(e)));
      } finally {
        setReady(true);
      }
    })();
    // Boot runs once; bootPanel is stable for the life of the provider.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const id = window.setInterval(() => void refresh(), 5_000);
    return () => window.clearInterval(id);
  }, [refresh]);

  const value = useMemo<CareStore>(
    () => ({
      ready,
      flow,
      mdnsName,
      clientURL,
      selectRole,
      clearRole,
      openStep,
      setOpenStep,
      stepsDone,
      setStepDone,
      run,
      setupReset,
      startInstall,
      resumeInstall,
      retryInstall,
      restartSetup,
      openPanel,
      tab,
      setTab,
      busy,
      busyLabel,
      operationError,
      clearOperationError,
      system,
      systemDetail,
      trouble,
      careUpdate,
      applyCareUpdate,
      dismissCareUpdate,
      installAppUpdate,
      acknowledgeAppUpdate,
      restorePending,
      version,
      platform,
      backups,
      backupsError,
      autostart,
      autostartReady,
      autostartSaving,
      autostartError,
      recheckAutostart: syncAutostart,
      storage,
      storageError,
      recheckStorage,
      refresh,
      reloadBackups,
      runAction,
      setAutostart,
      restore,
      restoreFile,
      uninstall,
      log,
    }),
    [
      ready, flow, mdnsName, clientURL, selectRole, clearRole, openStep, stepsDone, setStepDone,
      run, setupReset, startInstall, resumeInstall, retryInstall, restartSetup, openPanel,
      tab, busy, busyLabel, operationError, clearOperationError, system, systemDetail, trouble, careUpdate, applyCareUpdate, dismissCareUpdate,
      installAppUpdate, acknowledgeAppUpdate, restorePending, version, platform, backups, backupsError, autostart, autostartReady, autostartSaving, autostartError, storage, storageError, recheckStorage,
      refresh, reloadBackups, syncAutostart,
      runAction, setAutostart, restore, restoreFile, uninstall, log,
    ],
  );

  if (bootError) throw bootError;
  return <CareContext.Provider value={value}>{children}</CareContext.Provider>;
}
