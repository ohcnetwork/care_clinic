import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { bridge } from "@/lib/bridge";
import { useCare } from "@/state/care-store";
import type { DownloadInfo, ToolPlan } from "@/types";
import { SOFTWARE_DESCRIPTIONS } from "./setup-model";

export type CheckTone = "wait" | "ok" | "bad";
export type CheckId =
  | "residue"
  | "windows-vm"
  | "docker"
  | "disk"
  | "mdns"
  | "clinic"
  | "network";

/**
 * Which prerequisites are worth testing.
 *
 * "setup" is the first-run wizard. "running" is the same machinery pointed at a
 * clinic that is already installed and has stopped answering, so residue drops
 * out (after setup those "leftovers" are the live install). One row is added:
 * the clinic itself.
 */
export type ChecksMode = "setup" | "running";

/**
 * What the operator can press on a failing row. The wizard is used by people who
 * will not open a terminal, so a check that can't be acted on is a dead end -
 * every failure that we know how to fix carries the fix.
 *
 * run resolves to a confirmation to show when the work succeeded, or nothing
 * when there is none worth a dialog (opening Docker, opening a download page).
 */
export type CheckAction = {
  label: string;
  detail: string;
  run: () => Promise<string | void>;
  preview?: () => Promise<DownloadInfo>;
};

export type Check = {
  id: CheckId;
  title: string;
  detail: string;
  state: CheckTone;
  how: string;
  action?: CheckAction;
  blockedBy?: string;
};

type Result = { state: CheckTone; how: string; action?: CheckAction };

const WAITING: Result = { state: "wait", how: "" };

const NEEDS: Partial<Record<CheckId, CheckId[]>> = {
  docker: ["windows-vm"],
  residue: ["docker"],
  clinic: ["docker"],
};

function withPrerequisites(list: Check[]): Check[] {
  return list.map((check) => {
    const blocker = (NEEDS[check.id] ?? [])
      .map((id) => list.find((c) => c.id === id))
      .find((c) => c !== undefined && c.state !== "ok");
    return blocker ? { ...check, blockedBy: blocker.title } : check;
  });
}

function summarise(results: Result[]): CheckTone {
  if (results.some((r) => r.state === "bad")) return "bad";
  return results.some((r) => r.state === "wait") ? "wait" : "ok";
}

/**
 * Turns a plan from the host into a button. "manual" means we can't do it here,
 * so the button opens the vendor's download page instead of pretending.
 */
function actionFor(plan: ToolPlan, install: () => Promise<string | void>): CheckAction | undefined {
  if (plan.action === "" || plan.label === "") return undefined;
  if (plan.action === "manual") {
    return {
      label: plan.label,
      detail: plan.detail,
      run: () => bridge.OpenURL(plan.url),
    };
  }
  return {
    label: plan.label,
    detail: plan.detail,
    run: install,
    preview: plan.download_preview ? () => bridge.EngineDownloadInfo() : undefined,
  };
}

/**
 * The prerequisites the app can't bundle, one row each.
 */
export function useRequirementChecks(host: string, mode: ChecksMode = "setup") {
  const inSetup = mode === "setup";
  const { platform } = useCare();
  const engine = platform === "linux" ? "Docker" : "CARE engine";
  const [residue, setResidue] = useState<Result>(WAITING);
  const [windowsVM, setWindowsVM] = useState<Result | null>(null);
  const [docker, setDocker] = useState<Result>(WAITING);
  const [disk, setDisk] = useState<Result & { message: string }>({ ...WAITING, message: "" });
  const [nameCheck, setNameCheck] = useState<{ host: string; result: Result }>({ host: "", result: WAITING });
  const hostRef = useRef(host);
  hostRef.current = host;
  const nameRequest = useRef(0);
  const mdns = nameCheck.host === host ? nameCheck.result : WAITING;
  const [clinic, setClinic] = useState<Result>(WAITING);
  const [network, setNetwork] = useState<Result | null>(null);
  const [inFlight, setInFlight] = useState(0);

  /**
   * Leftovers from an earlier CARE Clinic. This is a hard blocker rather than a
   * warning: an old data volume still carrying the label gets re-attached by
   * compose, so the "new" clinic comes up holding the previous one's patients.
   *
   * Docker-side traces are invisible while Docker is down, so this is
   * deliberately re-run as part of recheckAll - once Docker goes green the scan
   * sees the containers, volumes, and images it could not see before.
   *
   * Until then the scan cannot answer at all, and the host says so rather than
   * reporting a clean machine. That is "unknown", not "dirty", so the row waits
   * on the Docker row instead of showing the operator a second red failure with
   * the same single cause. Waiting still blocks Continue, which needs every row
   * green, so the volume-reattachment blocker above is unaffected.
   */
  const checkResidue = useCallback(async (): Promise<Result> => {
    setResidue(WAITING);
    let result: Result;
    try {
      const report = await bridge.ScanResidue();
      result = report.clean
        ? { state: "ok", how: "" }
        : {
            state: "bad",
            how: report.traces?.length
              ? `Found ${report.traces.map((t) => t.label.toLowerCase()).join(", ")}. These must be removed before a new clinic can be set up.`
              : "The earlier installation still needs cleanup before a new clinic can be set up.",
            action: {
              label: "Remove old installation",
              detail:
                "Deletes the earlier installation's clinic data, images, and settings from this computer. Backups are kept.",
              run: async () => {
                await bridge.PurgeResidue(true);
              },
            },
          };
    } catch (e) {
      const docker = await bridge.DockerStatus().catch(() => null);
      result =
        docker && !docker.ok
          ? { state: "wait", how: `Waiting for ${engine} before this can be checked.` }
          : { state: "bad", how: String(e) };
    }
    setResidue(result);
    return result;
  }, [engine]);

  const checkWindowsVM = useCallback(async (): Promise<Result | null> => {
    let result: Result | null;
    try {
      const status = await bridge.WindowsVMStatus();
      result = status.applicable
        ? {
            state: status.ok ? "ok" : "bad",
            how: status.ok ? "" : status.how || status.message,
            action:
              status.ok || !status.fixable
                ? undefined
                : {
                    label: "Turn on Windows Hypervisor Platform",
                    detail:
                      "Turns on Windows Hypervisor Platform, the Windows feature the CARE engine runs on. Windows will ask for permission and usually needs a restart.",
                    run: () => bridge.EnableWindowsVM(),
                  },
          }
        : null;
    } catch {
      result = null;
    }
    setWindowsVM(result);
    return result;
  }, []);

  const checkDocker = useCallback(async (): Promise<Result> => {
    setDocker(WAITING);
    let result: Result;
    try {
      const status = await bridge.DockerStatus();
      if (status.ok) {
        result = { state: "ok", how: "" };
      } else {
        const plan = await bridge.DockerPlan();
        result = {
          state: "bad",
          how: status.message,
          action: actionFor(plan, () =>
            plan.action === "open" ? bridge.OpenDocker() : bridge.InstallDocker(),
          ),
        };
      }
    } catch (e) {
      result = { state: "bad", how: String(e) };
    }
    setDocker(result);
    return result;
  }, []);

  const checkDisk = useCallback(async (): Promise<Result> => {
    setDisk({ ...WAITING, message: "" });
    let result: Result & { message: string };
    try {
      const status = await bridge.DiskStatus();
      const ok = status.ok && status.need > 0 && status.free >= status.need;
      result = {
        state: ok ? "ok" : "bad",
        how: ok ? "" : status.need === 0 ? "Couldn't measure the free space. Check again before continuing." : status.how || status.message,
        message: status.message,
      };
    } catch (e) {
      result = { state: "bad", how: String(e), message: "" };
    }
    setDisk(result);
    return result;
  }, []);

  /**
   * Is the clinic actually serving? Only meaningful once it is installed, which
   * is why it carries a Start button rather than an install one.
   */
  const checkClinic = useCallback(async (): Promise<Result> => {
    setClinic(WAITING);
    let result: Result;
    try {
      const health = await bridge.ClinicHealth();
      result = health.active
        ? { state: "ok", how: "" }
        : {
            state: "bad",
            how: health.detail || "The clinic isn't answering on this computer.",
            action: {
              label: "Start clinic",
              detail: "Starts the clinic software. This takes about a minute.",
              run: () => bridge.ClinicAction("start", ""),
            },
          };
    } catch (e) {
      result = { state: "bad", how: String(e) };
    }
    setClinic(result);
    return result;
  }, []);

  const checkMDNS = useCallback(async (): Promise<Result> => {
    const requestedHost = hostRef.current;
    const request = ++nameRequest.current;
    setNameCheck({ host: requestedHost, result: WAITING });
    let result: Result;
    try {
      const status = await bridge.MDNSStatus(requestedHost);
      result = {
        state: status.ok ? "ok" : "bad",
        how: status.ok ? "" : status.message,
      };
    } catch (e) {
      result = { state: "bad", how: String(e) };
    }
    if (request !== nameRequest.current || requestedHost !== hostRef.current) return WAITING;
    setNameCheck({ host: requestedHost, result });
    return result;
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => void checkMDNS(), 400);
    const poll = window.setInterval(() => void checkMDNS(), 30_000);
    return () => {
      window.clearTimeout(timer);
      window.clearInterval(poll);
      nameRequest.current++;
    };
  }, [host, checkMDNS]);

  // Windows-only gate; elsewhere the host reports it as not applicable and the
  // row is hidden rather than shown as passing.
  const checkNetwork = useCallback(async (): Promise<Result | null> => {
    let result: Result | null;
    try {
      const status = await bridge.NetworkStatus();
      result = status.applicable
        ? {
            state: status.ok ? "ok" : "bad",
            how: status.ok ? "" : status.how || status.message,
            action:
              status.ok || !status.fixable
                ? undefined
                : {
                    label: "Fix automatically",
                    detail:
                      "Windows has this network locked down. It will be updated so other devices on this WiFi can reach the clinic on this computer.",
                    run: () => bridge.FixNetwork(),
                  },
          }
        : null;
    } catch {
      result = null;
    }
    setNetwork(result);
    return result;
  }, []);

  const recheckAll = useCallback(async (): Promise<CheckTone> => {
    setInFlight((n) => n + 1);
    try {
      const [r, w, d, s, m, c, n] = await Promise.all([
        inSetup ? checkResidue() : null,
        checkWindowsVM(),
        checkDocker(),
        checkDisk(),
        checkMDNS(),
        inSetup ? null : checkClinic(),
        checkNetwork(),
      ]);
      return summarise([r, w, d, s, m, c, n].filter((x): x is Result => x !== null));
    } finally {
      setInFlight((n) => n - 1);
    }
  }, [
    inSetup,
    checkResidue,
    checkWindowsVM,
    checkDocker,
    checkDisk,
    checkMDNS,
    checkClinic,
    checkNetwork,
  ]);

  useEffect(() => {
    void recheckAll();
  }, [recheckAll]);

  const checks = useMemo<Check[]>(() => {
    const list: Check[] = [];
    if (inSetup) {
      list.push({
        id: "residue",
        title: "A clean computer",
        detail: "Nothing left from an earlier CARE Clinic",
        ...residue,
      });
    }
    if (windowsVM) {
      list.push({
        id: "windows-vm",
        title: "Windows Hypervisor Platform",
        detail: "The Windows feature the CARE engine runs on",
        ...windowsVM,
      });
    }
    list.push({
      id: "docker",
      title: engine,
      detail: SOFTWARE_DESCRIPTIONS.docker,
      ...docker,
    });
    const { message: diskMessage, ...diskResult } = disk;
    list.push({
      id: "disk",
      title: "Free disk space",
      detail: [
        inSetup ? "At least 30 GB for the clinic software and data" : "Room for the clinic's data",
        diskMessage,
      ]
        .filter(Boolean)
        .join(" · "),
      ...diskResult,
    });
    list.push({
      id: "mdns",
      title: inSetup ? "Clinic address availability" : "Network name",
      detail: `${host} on the clinic WiFi`,
      ...mdns,
    });
    if (!inSetup) {
      list.push({
        id: "clinic",
        title: "Clinic software",
        detail: "Serving the clinic to your staff",
        ...clinic,
      });
    }
    if (network) {
      list.push({
        id: "network",
        title: "Network profile",
        detail: "Other devices can reach this computer",
        ...network,
      });
    }
    return withPrerequisites(list);
  }, [inSetup, clinic, disk, docker, engine, host, mdns, network, residue, windowsVM]);

  const overall = useMemo(
    () => summarise(checks.map((c) => ({ state: c.state, how: c.how }))),
    [checks],
  );

  return { checks, overall, checking: inFlight > 0, recheckAll, checkMDNS };
}
