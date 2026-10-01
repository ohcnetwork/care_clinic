import { useCallback, useEffect, useRef, useState } from "react";

import { bridge, onCareEvent } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { AppUpdate, AppUpdateProgress } from "@/types";

type UpdateProblem =
  | { kind: "offline" | "check" | "download" | "install" }
  | { kind: "unavailable"; version: string };

function updateProblem(error: unknown, checking: boolean): UpdateProblem {
  const text = errorText(error);
  const unavailable = /^release (\S+) has no installer for this computer$/.exec(text);
  if (unavailable) return { kind: "unavailable", version: unavailable[1] };
  if (checking) {
    return { kind: /^couldn't reach GitHub to check for updates:/i.test(text) ? "offline" : "check" };
  }
  return {
    kind: /update didn't download properly and was deleted without being installed/i.test(text)
      ? "download"
      : "install",
  };
}

export function useAppUpdate(disabled = false, enabled = true, isBlocked?: () => boolean) {
  const { busy, busyLabel, version, installAppUpdate, acknowledgeAppUpdate, log } = useCare();
  const [update, setUpdate] = useState<AppUpdate | null>(null);
  const [checking, setChecking] = useState(false);
  const [problem, setProblem] = useState<UpdateProblem | null>(null);
  const [progress, setProgress] = useState<AppUpdateProgress | null>(null);
  const [starting, setStarting] = useState(false);
  const mounted = useRef(false);
  const checkPending = useRef(false);
  const installPending = useRef(false);
  const progressRef = useRef<AppUpdateProgress | null>(null);
  const failure = useRef<unknown>(null);
  const updating = busy && busyLabel === "Updating CARE Desktop";
  const active = starting || updating || progress !== null;
  const isActive = () => installPending.current || progressRef.current !== null || updating;

  const check = useCallback(async () => {
    if (checkPending.current || installPending.current || progressRef.current) return;
    checkPending.current = true;
    setChecking(true);
    setProblem(null);
    setUpdate(null);
    try {
      const result = await bridge.CheckAppUpdate();
      if (mounted.current) setUpdate(result);
    } catch (error) {
      log(`check CARE Desktop update: ${errorText(error)}`);
      if (mounted.current) setProblem(updateProblem(error, true));
    } finally {
      checkPending.current = false;
      if (mounted.current) setChecking(false);
    }
  }, [log]);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    const offProgress = onCareEvent("app-update-progress", (next: AppUpdateProgress) => {
      progressRef.current = next;
      setProgress(next);
      setStarting(false);
    });
    const offError = onCareEvent("care-error", (label: string, detail: string) => {
      if (label === "The CARE Desktop update didn't finish") failure.current = detail;
    });
    const offDone = onCareEvent("care-done", (code: number, label?: string) => {
      if (label !== "app-update") return;
      installPending.current = false;
      setStarting(false);
      if (code !== 0) {
        progressRef.current = null;
        setProgress(null);
        setProblem(updateProblem(failure.current, false));
      }
    });
    return () => {
      offProgress();
      offError();
      offDone();
    };
  }, []);

  useEffect(() => {
    if (enabled) void check();
    // Check once on entry, not after every update/busy transition.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, check]);

  const install = async () => {
    if (disabled || busy || isBlocked?.() || checkPending.current || installPending.current || progressRef.current) return;
    installPending.current = true;
    failure.current = null;
    setProblem(null);
    setStarting(true);
    try {
      await installAppUpdate();
      // The promise only accepts the job. Events, not its resolution, finish it.
    } catch (error) {
      installPending.current = false;
      if (mounted.current) {
        setStarting(false);
        setProblem(updateProblem(error, false));
      }
    }
  };

  const dismiss = () => {
    if (installPending.current ||
      (progressRef.current && progressRef.current.phase !== "installer") ||
      !acknowledgeAppUpdate()) return;
    progressRef.current = null;
    setProgress(null);
    setProblem(null);
    setUpdate(null);
  };

  return {
    update,
    version: update?.current || version,
    checking,
    problem,
    progress,
    active,
    isActive,
    updating,
    disabled: disabled || busy,
    check,
    install,
    dismiss,
  };
}

export type AppUpdateController = ReturnType<typeof useAppUpdate>;
