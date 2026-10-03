import { useCallback, useEffect, useRef, useState } from "react";

import { Callout, isClinicName, LogButton } from "@/components/onboarding";
import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { useAppUpdate, type AppUpdateController } from "@/hooks/use-app-update";
import { usePasswordStrength } from "@/hooks/use-password-strength";
import { bridge, onCareEvent } from "@/lib/bridge";
import { diskSize, errorText, normaliseHost } from "@/lib/format";
import { downloadProblem, type PrerequisiteProblem } from "@/lib/prerequisite-errors";
import { useCare } from "@/state/care-store";
import type { SetupForm } from "@/state/forms";
import type { BackupSpace, PrereqDownloadProgress, ResidueReport, RestartPlan, SetupIssue, SetupPage, ToolPlan } from "@/types";
import { AddressStep, AdminStep, BackupStep, ReviewStep, type AddressResult } from "./configuration-steps";
import { RequirementStep, type RequirementAction } from "./requirement-step";
import { RestartDialog } from "./restart-dialog";
import { SetupLayout } from "./setup-layout";
import { backupProblem, EMPTY_RECOVERY, isRequirement, recoveryProblem, SETUP_LABELS, type RequirementPage } from "./setup-model";
import { useSetupChecks } from "./use-setup-checks";

const ALL_STEPS: SetupPage[] = ["space", "windows", "software", "cleanup", "network", "address", "backup", "admin", "review", "install"];

export function SetupScreen({ form, patch }: { form: SetupForm; patch: (values: Partial<SetupForm>) => void }) {
  const { clearRole, log } = useCare();
  const [status, setStatus] = useState<"starting" | "ready" | "failed">("starting");
  const [attempt, setAttempt] = useState(0);
  const [localBusy, setLocalBusy] = useState(false);
  const busyRef = useRef(true);
  const pending = useRef<Promise<void> | null>(null);
  const update = useAppUpdate(localBusy || status !== "ready", status === "ready", () => busyRef.current);
  const onBusy = useCallback((value: boolean) => { busyRef.current = value; setLocalBusy(value); }, []);
  useEffect(() => {
    let live = true;
    pending.current ??= bridge.BeginServerSetup();
    void pending.current.then(
      () => { if (live) setStatus("ready"); },
      (e) => { log(`begin setup: ${errorText(e)}`); if (live) setStatus("failed"); },
    );
    return () => { live = false; };
  }, [attempt, log]);
  if (status === "ready") return <SetupWizard form={form} patch={patch} update={update} onBusy={onBusy} />;
  return <SetupLayout steps={ALL_STEPS} page="space" done={{}} working={status === "starting" || localBusy}
    title="Room for the clinic" subtitle="Preparing this computer for CARE." note={status === "starting" ? "Preparing setup…" : "Try again before continuing."}
    back={() => { setLocalBusy(true); void clearRole().finally(() => setLocalBusy(false)); }} update={update}>
    {status === "starting" ? <div className="on-card on-pad on-row" role="status"><Spinner />Preparing setup…</div>
      : <Callout tone="danger" title="Couldn't start setup">CARE Desktop couldn't prepare setup on this computer. Try again. If it keeps happening, share the log file.
        <div className="on-actions"><Button disabled={localBusy} onClick={() => { pending.current = null; setStatus("starting"); setAttempt((n) => n + 1); }}>Try again</Button><LogButton /></div>
      </Callout>}
  </SetupLayout>;
}

function SetupWizard({ form, patch, update, onBusy }: {
  form: SetupForm; patch: (values: Partial<SetupForm>) => void; update: AppUpdateController; onBusy: (busy: boolean) => void;
}) {
  const care = useCare();
  const requirements = useSetupChecks();
  const [page, setPage] = useState<SetupPage>("space");
  const pageRef = useRef<SetupPage>("space");
  const [editing, setEditing] = useState(false);
  const editingRef = useRef(false);
  const [done, setDone] = useState<Partial<Record<SetupPage, boolean>>>({});
  const [operation, setOperation] = useState("preparing");
  const workRef = useRef(false);
  const [tool, setTool] = useState("");
  const [download, setDownload] = useState<PrereqDownloadProgress | null>(null);
  const [problem, setProblem] = useState<PrerequisiteProblem | null>(null);
  const [actionNote, setActionNote] = useState("");
  const [cleanupBefore, setCleanupBefore] = useState<ResidueReport | null>(null);
  const [restart, setRestartState] = useState<RestartPlan | null>(null);
  const restartRef = useRef<RestartPlan | null>(null);
  const [recovery, setRecovery] = useState(EMPTY_RECOVERY);
  const [recoveryError, setRecoveryError] = useState("");
  const [passwordError, setPasswordError] = useState("");
  const [space, setSpace] = useState<BackupSpace | null>(null);
  const [folderError, setFolderError] = useState("");
  const [address, setAddress] = useState<AddressResult>({ name: "", state: "waiting", message: "" });
  const [addressPending, setAddressPending] = useState(0);
  const [addressQueued, setAddressQueued] = useState(false);
  const addressQueuedRef = useRef(false);
  const addressSequence = useRef(0);
  const addressTimer = useRef(0);
  const addressTasks = useRef(new Set<Promise<boolean>>());
  const addressWrite = useRef(Promise.resolve());
  const [issues, setIssues] = useState<SetupIssue[]>([]);
  const [verified, setVerified] = useState(false);
  const [autoPoll, setAutoPoll] = useState<RequirementPage | null>(null);
  const formRef = useRef(form);
  formRef.current = form;
  const mounted = useRef(false);
  const initialise = useRef<Promise<void> | null>(null);
  const strength = usePasswordStrength(form.adminPassword);
  const busy = operation !== "" || addressQueued || addressPending > 0 || care.busy || restart !== null;
  const locked = busy || update.active;
  const backupReady = !!space && !folderError && recovery.backup_verified && !recovery.backup_problem && !recoveryError;
  const adminReady = strength.strong && !!form.adminConfirm && form.adminPassword === form.adminConfirm && !passwordError && !folderError && recovery.codes_saved && !recovery.codes_problem && !recoveryError;
  const addressReady = address.state === "ready" && address.name === normaliseHost(form.hostInput) && isClinicName(form.hostInput);
  const engine = care.platform === "linux" ? "Docker" : "Rancher Desktop";
  const syncBusy = useCallback(() => {
    onBusy(workRef.current || addressQueuedRef.current || addressTasks.current.size > 0 || restartRef.current !== null);
  }, [onBusy]);
  const setRestart = (value: RestartPlan | null) => {
    restartRef.current = value;
    setRestartState(value);
    syncBusy();
  };

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      addressSequence.current++;
      window.clearTimeout(addressTimer.current);
    };
  }, []);
  useEffect(() => onCareEvent("prereq-download-progress", (value: PrereqDownloadProgress) => {
    if (workRef.current) setDownload(value);
  }), []);
  useEffect(() => care.setStepDone("backup", backupReady), [backupReady, care.setStepDone]);
  useEffect(() => care.setStepDone("admin", adminReady), [adminReady, care.setStepDone]);

  const stepsNow = (): SetupPage[] => ALL_STEPS.filter((step) => {
    const values = requirements.current.current;
    if (step === "windows") return values.windows.value?.status.applicable !== false;
    if (step === "network") return values.network.value?.applicable !== false;
    if (step === "cleanup") return care.platform === "windows" || requirements.hadResidue.current || values.cleanup.value?.clean !== true;
    return true;
  });
  const steps = stepsNow();
  const markDone = (step: SetupPage, value: boolean) => setDone((prev) => ({ ...prev, [step]: value }));
  const changeForm = (values: Partial<SetupForm>) => {
    formRef.current = { ...formRef.current, ...values };
    patch(values);
    setVerified(false);
    if (values.adminPassword !== undefined || values.adminConfirm !== undefined) setPasswordError("");
  };
  const loadRecovery = async () => {
    const status = await bridge.GetSetupRecoveryStatus();
    setRecovery(status);
    setRecoveryError("");
    return status;
  };
  const checkFolder = async (dir: string) => {
    try {
      const [raw, result] = await Promise.all([bridge.ValidateBackupDir(dir), bridge.BackupDirSpace(dir)]);
      if (raw) care.log(`backup location: ${raw}`);
      setFolderError(backupProblem(raw));
      setSpace(result);
      return raw === "";
    } catch (e) {
      setSpace(null);
      setFolderError("That location couldn't be checked. Check it again, or choose a different location.");
      throw e;
    }
  };

  const checkAddress = useCallback((raw: string): Promise<boolean> => {
    const sequence = ++addressSequence.current;
    const name = normaliseHost(raw);
    onBusy(true);
    setAddress({ name, state: "checking", message: "Checking whether this name is free on your network…" });
    setAddressPending((n) => n + 1);
    const task = (async () => {
      const current = () => mounted.current && sequence === addressSequence.current;
      try {
        if (!isClinicName(raw)) {
          if (current()) setAddress({ name, state: "bad", message: "Use a name of 1 to 63 letters, numbers or hyphens. Start and end with a letter or number." });
          return false;
        }
        const invalid = await bridge.ValidateDomain(raw.trim());
        if (!current()) return false;
        if (invalid) {
          care.log(`clinic address: ${invalid}`);
          setAddress({ name, state: "bad", message: "Choose a name with letters, numbers or hyphens. Start and end with a letter or number." });
          return false;
        }
        const result = await bridge.MDNSStatus(name);
        if (!current()) return false;
        if (!result.ok) {
          care.log(`clinic address: ${result.message}`);
          const taken = /already in use|already taken/i.test(result.message);
          setAddress({ name, state: taken ? "bad" : "failed", message: taken
            ? `${name} is already taken on this network — type a different name.`
            : "Couldn't check this name on the network. Keep this computer connected and check again." });
          return false;
        }
        const saved = addressWrite.current.then(async () => {
          if (!current()) return false;
          await bridge.SetMDNSName(name);
          return current();
        });
        addressWrite.current = saved.then(() => {}, () => {});
        if (!(await saved)) return false;
        setAddress({ name, state: "ready", message: `${name} is free to use on this network.` });
        return true;
      } catch (e) {
        care.log(`clinic address: ${errorText(e)}`);
        if (current()) setAddress({ name, state: "failed", message: "Couldn't check or save this clinic address. Try again, or share the log file with your support contact." });
        return false;
      } finally { if (mounted.current) setAddressPending((n) => n - 1); }
    })();
    addressTasks.current.add(task);
    void task.then(() => { addressTasks.current.delete(task); syncBusy(); });
    return task;
  }, [care.log, onBusy, syncBusy]);

  const validateReview = async () => {
    setVerified(false);
    const latest = formRef.current;
    const [status, backup] = await Promise.all([bridge.GetSetupRecoveryStatus(), bridge.BackupDirSpace(latest.backupDir)]);
    setRecovery(status);
    setSpace(backup);
    const found = await bridge.ValidateSetup(latest.hostInput.trim(), latest.adminPassword, latest.backupDir);
    const result = [...found];
    if (!isClinicName(latest.hostInput) && !result.some((issue) => issue.step === "address")) {
      result.push({ step: "address", message: "Choose a valid clinic name before installing." });
    }
    if (latest.adminPassword !== latest.adminConfirm && !result.some((issue) => issue.step === "admin")) {
      result.push({ step: "admin", message: "Both passwords must match before installing." });
    }
    if (result.some((issue) => issue.step === "cleanup")) requirements.hadResidue.current = true;
    setIssues(result);
    setVerified(true);
    setRecoveryError("");
    care.setStepDone("checks", !result.some((issue) => isRequirement(issue.step) || issue.step === "address"));
    for (const issue of result) markDone(issue.step, false);
    return result;
  };

  const nextAfter = (from: SetupPage) => stepsNow().find((candidate) => ALL_STEPS.indexOf(candidate) > ALL_STEPS.indexOf(from)) ?? "review";

  const visit = async (destination: SetupPage, mode: "forward" | "back" | "edit" = "forward") => {
    setAutoPoll(null);
    setActionNote("");
    let target = destination;
    for (;;) {
      if (!mounted.current) return;
      pageRef.current = target;
      editingRef.current = mode === "edit" && target !== "review";
      setPage(target);
      setEditing(editingRef.current);
      care.setOpenStep(isRequirement(target) || target === "address" ? "checks" : target === "review" ? "install" : target === "backup" ? "backup" : "admin");
      if (isRequirement(target)) {
        const ok = await requirements.check(target);
        markDone(target, ok);
        if (target === "windows" && requirements.current.current.windows.value?.restart.needed) {
          setRestart(requirements.current.current.windows.value.restart);
        }
        if (!ok || mode === "back") return;
        target = mode === "edit" ? "review" : nextAfter(target);
        continue;
      }
      if (target === "address") { await checkAddress(formRef.current.hostInput); return; }
      if (target === "backup") { await loadRecovery(); await checkFolder(formRef.current.backupDir); return; }
      if (target === "admin") { await loadRecovery(); return; }
      if (target === "review") { await validateReview(); return; }
      return;
    }
  };

  const execute = async (name: string, fn: () => Promise<void>, recoveryAction = false) => {
    if (workRef.current || addressQueuedRef.current || addressTasks.current.size || restartRef.current || update.isActive() || care.busy) return;
    workRef.current = true;
    onBusy(true);
    setOperation(name);
    setProblem(null);
    if (recoveryAction) setRecoveryError("");
    try { await fn(); } catch (e) {
      care.log(`setup ${name}: ${errorText(e)}`);
      if (recoveryAction) setRecoveryError(recoveryProblem(errorText(e)));
      else setProblem({ message: "This step couldn't finish. Try again. If it keeps happening, share the log file with your support contact." });
    } finally {
      workRef.current = false;
      syncBusy();
      if (mounted.current) { setOperation(""); setTool(""); }
    }
  };

  useEffect(() => {
    // One bootstrap across StrictMode effect replay. All mutations wait for
    // BeginServerSetup in the parent; these applicability checks are read-only.
    initialise.current ??= (async () => {
      workRef.current = true;
      try {
        await Promise.all([requirements.check("windows"), requirements.check("network")]);
        await visit("space");
      } catch (e) {
        care.log(`prepare setup: ${errorText(e)}`);
        setProblem({ message: "Couldn't read the saved setup. Check this step again before continuing." });
      } finally { workRef.current = false; syncBusy(); if (mounted.current) setOperation(""); }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const recheck = () => void execute("checking", async () => {
    if (isRequirement(pageRef.current)) {
      const target = pageRef.current;
      const ok = await requirements.check(target);
      markDone(target, ok);
      if (ok) await visit(editingRef.current ? "review" : nextAfter(target));
      else if (target === "windows" && requirements.current.current.windows.value?.restart.needed) setRestart(requirements.current.current.windows.value.restart);
    }
  });

  const perform = (action: RequirementAction) => void execute(action, async () => {
    setAutoPoll(null);
    setDownload(null);
    setActionNote("");
    const target = pageRef.current;
    if (!isRequirement(target)) return;
    try {
      if (action === "windows") { setTool("windows"); await bridge.InstallWSL(); }
      else if (action === "network") { setTool("network"); await bridge.FixNetwork(); }
      else if (action === "cleanup") {
        setTool("cleanup");
        setCleanupBefore(requirements.current.current.cleanup.value);
        await bridge.PurgeResidue(true);
        await loadRecovery();
        setDone((prev) => ({ ...prev, backup: false, admin: false }));
      } else {
        const runTool = async (id: "docker" | "git", plan: ToolPlan | null) => {
          if (!plan || !plan.action) throw new Error(`No supported action for ${id}.`);
          setTool(id);
          setDownload(null);
          if (plan.action === "manual") {
            await bridge.OpenURL(plan.url);
            setActionNote(`Follow the instructions that opened to install ${id === "docker" ? engine : "Git"}, then check again.`);
          } else if (id === "docker") {
            if (plan.action === "open") await bridge.OpenDocker();
            else await bridge.InstallDocker();
          } else {
            await bridge.InstallGit();
            if (care.platform === "darwin") setActionNote("Your Mac may have opened an installation window. Choose Install there. CARE checks again automatically.");
          }
        };
        let status = requirements.current.current.software.value;
        if (!status) throw new Error("The required software has not been checked.");
        if (!status.docker.ok && action !== "git") {
          await runTool("docker", status.dockerPlan);
          await requirements.check("software");
          status = requirements.current.current.software.value;
          if (!status?.docker.ok) {
            const plan = await bridge.RestartPlan();
            if (plan.needed) setRestart(plan);
            setAutoPoll("software");
            return;
          }
        }
        if (status && !status.git.ok && action !== "docker") await runTool("git", status.gitPlan);
      }
      setTool("");
      const ok = await requirements.check(target);
      markDone(target, ok);
      if (ok) await visit(editingRef.current ? "review" : nextAfter(target));
      else if (target === "software") setAutoPoll("software");
      else if (target === "windows" && requirements.current.current.windows.value?.restart.needed) setRestart(requirements.current.current.windows.value.restart);
    } catch (e) {
      care.log(`setup ${action}: ${errorText(e)}`);
      const text = errorText(e);
      setProblem(action === "cleanup"
        ? { message: "Cleanup didn't finish. Check what's still listed and try again. Approve the request if your computer asks for permission." }
        : downloadProblem(e) ?? { message: /cancel|denied|permission|-128/i.test(text)
          ? "Your computer didn't approve the change. Try again and approve the request when your computer asks."
          : "The required change couldn't finish. Try again. If it keeps failing, send the log file to your support contact." });
      await requirements.check(target);
      if (action === "cleanup") await loadRecovery();
    }
  });

  useEffect(() => {
    if (!autoPoll || page !== autoPoll || update.active) return;
    const timer = window.setInterval(() => {
      if (!workRef.current) recheck();
    }, 5_000);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoPoll, page, update.active]);

  const changeHost = (value: string) => {
    changeForm({ hostInput: value });
    addressQueuedRef.current = true;
    onBusy(true);
    setAddressQueued(true);
    addressSequence.current++;
    window.clearTimeout(addressTimer.current);
    setAddress({ name: normaliseHost(value), state: "checking", message: "Checking whether this name is free on your network…" });
    markDone("address", false);
    addressTimer.current = window.setTimeout(() => {
      addressQueuedRef.current = false;
      setAddressQueued(false);
      void checkAddress(value);
    }, 350);
  };

  const goBack = () => void execute("back", async () => {
    window.clearTimeout(addressTimer.current);
    await Promise.all(addressTasks.current);
    if (editingRef.current) { await visit("review"); return; }
    const visible = stepsNow();
    const index = visible.indexOf(pageRef.current);
    if (index <= 0) await care.clearRole();
    else await visit(visible[index - 1], "back");
  });

  const continueSetup = () => void execute("continue", async () => {
    window.clearTimeout(addressTimer.current);
    const target = pageRef.current;
    const latest = formRef.current;
    if (target === "review") {
      if ((await validateReview()).length) return;
      try {
        await care.startInstall({
          host: normaliseHost(latest.hostInput),
          adminPassword: latest.adminPassword,
          backupDir: latest.backupDir,
          pages: stepsNow(),
        });
      } catch (e) {
        care.log(`start installation: ${errorText(e)}`);
        const failures = await validateReview();
        if (!failures.length) setProblem({ message: "Installation hasn't started. CARE couldn't accept the request. Wait a moment and try again, or share the log file." });
      }
      return;
    }
    let ok = false;
    if (isRequirement(target)) ok = await requirements.check(target);
    else if (target === "address") ok = await checkAddress(latest.hostInput);
    else if (target === "backup") {
      const folderOk = await checkFolder(latest.backupDir);
      const status = await loadRecovery();
      ok = folderOk && status.backup_verified && !status.backup_problem;
      if (!ok && folderOk) setRecoveryError("Save and check the correct recovery file before continuing.");
    } else if (target === "admin") {
      const invalid = await bridge.ValidatePassword(latest.adminPassword);
      const status = await loadRecovery();
      ok = !invalid && latest.adminPassword === latest.adminConfirm && status.codes_saved && !status.codes_problem;
      if (invalid || latest.adminPassword !== latest.adminConfirm) setPasswordError("Choose and confirm a valid password before continuing.");
      else if (!ok) setRecoveryError("Save your recovery codes before continuing.");
    }
    markDone(target, ok);
    if (ok) await visit(editingRef.current ? "review" : nextAfter(target));
  });

  const chooseFolder = () => void execute("choose-folder", async () => {
    const chosen = await bridge.ChooseFolder("Choose backup folder");
    if (!chosen) return;
    changeForm({ backupDir: chosen });
    setSpace(null);
    await checkFolder(chosen);
  });
  const saveRecovery = (kind: "save-backup" | "verify-backup" | "replace-backup" | "save-codes") => void execute(kind, async () => {
    const latest = formRef.current;
    if (!(await checkFolder(latest.backupDir))) return;
    if (kind === "save-codes") {
      const invalid = await bridge.ValidatePassword(latest.adminPassword);
      if (invalid || latest.adminPassword !== latest.adminConfirm) {
        setPasswordError("Choose and confirm a valid password before saving the recovery codes.");
        return;
      }
    }
    const changed = kind === "save-backup" ? await bridge.SaveSetupBackupRecovery(latest.backupDir)
      : kind === "replace-backup" ? await bridge.ReplaceSetupBackupRecovery(latest.backupDir)
      : kind === "verify-backup" ? await bridge.VerifySetupBackupRecovery(latest.backupDir)
      : await bridge.SaveAdminRecoveryCodes("", latest.backupDir);
    if (changed) await loadRecovery();
  }, true);
  const reloadRecovery = () => void execute("read-recovery", async () => { await loadRecovery(); }, true);
  const edit = (target: SetupPage) => void execute("edit", () => visit(target, "edit"));

  const titles: Record<SetupPage, [string, string]> = {
    space: ["Room for the clinic", requirements.checks.space.value?.need ? `CARE needs about ${diskSize(requirements.checks.space.value.need)} on this computer — the software, your clinic's records, and room for the backups it makes.` : "Checking room for the software, your clinic's records, and the backups CARE makes."],
    windows: ["Getting Windows ready", "Windows needs two changes before CARE can run, and CARE makes both for you. This is the first."],
    software: ["Installing what CARE needs", `CARE needs ${engine} and Git. Keep this computer connected and awake — you can leave it running and come back.`],
    cleanup: ["Removing stale files from an earlier setup", "A previous CARE setup left files or settings behind. They have to go before a new clinic can be installed here."],
    network: ["Setting this network to Private", "The second of the two Windows changes. Staff computers, phones and tablets open CARE from here."],
    address: ["Choosing the clinic address", "Staff type this into their browser to open CARE. Short and easy to say out loud works best."],
    backup: ["Setting up backups", "CARE makes an encrypted backup automatically, once every 24 hours. Choose where the backups go, then save the file that unlocks them."],
    admin: ["Creating the admin password", "This password protects CARE Desktop on this computer and is the first sign-in for CARE itself."],
    review: ["Review before installing", "Here's what CARE will set up."],
    install: ["Installing CARE", ""],
  };
  const ready = isRequirement(page) ? requirements.checks[page].state === "ready"
    : page === "address" ? addressReady : page === "backup" ? backupReady : page === "admin" ? adminReady
    : verified && issues.length === 0;
  const note = busy ? page === "review" ? "Checking everything before installation…" : tool ? "Keep this window open while the change finishes…" : "Checking…"
    : problem ? "Check this step again before continuing."
    : editing ? "Return straight to Review when this step is ready."
    : page === "address" ? addressReady ? "Next: backups." : "Pick an address that is free to continue."
    : page === "backup" ? backupReady ? "Backups will be made every 24 hours." : "Save and check your recovery file to continue."
    : page === "admin" ? adminReady ? "Almost done — one last look before installing." : "Choose a password and save your recovery codes to continue."
    : page === "review" ? issues.length ? "Fix the highlighted step; you come back to this screen." : "Nothing has been installed yet."
    : ready ? "Ready to continue." : `Finish ${SETUP_LABELS[page].toLowerCase()} to continue.`;
  const blocked = [...issues.map((issue) => issue.step)];
  if (isRequirement(page) && ["blocked", "failed"].includes(requirements.checks[page].state)) blocked.push(page);

  return (
    <SetupLayout steps={steps} page={page} done={done} blocked={blocked} working={busy}
      title={titles[page][0]} subtitle={titles[page][1]} note={note} back={page === "review" ? undefined : goBack}
      next={continueSetup} nextDisabled={!ready} editing={editing} update={update}>
      <RestartDialog plan={restart} onDismiss={() => setRestart(null)} />
      {isRequirement(page) ? <RequirementStep page={page} checks={requirements.checks} busy={locked} tool={tool} download={download}
        actionError={problem} actionNote={actionNote} cleanupBefore={cleanupBefore} onAction={perform} onCheck={recheck}
        onRestart={() => setRestart(requirements.current.current.windows.value?.restart ?? null)} />
        : page === "address" ? <AddressStep value={form.hostInput} result={address} disabled={operation !== "" || update.active || care.busy} onChange={changeHost} onCheck={() => void checkAddress(formRef.current.hostInput)} />
        : page === "backup" ? <BackupStep form={form} space={space} folderProblem={folderError} recovery={recovery} recoveryError={recoveryError} busy={locked} action={operation}
          onOpenFolder={() => void execute("open-backup-folder", () => bridge.OpenSetupRecoveryFolder(false))}
          onChoose={chooseFolder} onCheck={() => void execute("check-folder", async () => { await checkFolder(formRef.current.backupDir); })}
          onSave={() => saveRecovery("save-backup")} onVerify={() => saveRecovery("verify-backup")} onReplace={() => saveRecovery("replace-backup")} onReload={reloadRecovery} />
        : page === "admin" ? <AdminStep form={form} patch={changeForm} strength={strength} passwordError={passwordError} folderProblem={folderError} recovery={recovery} recoveryError={recoveryError} busy={locked} action={operation}
          onOpenFolder={() => void execute("open-codes-folder", () => bridge.OpenSetupRecoveryFolder(true))}
          onSave={() => saveRecovery("save-codes")} onPrint={() => void execute("open-codes", () => bridge.OpenSetupRecoveryCodes(), true)} onReload={reloadRecovery}
          onBackups={() => void execute("backup-location", () => visit("backup", editingRef.current ? "edit" : "back"))} />
        : <ReviewStep steps={steps} form={form} backupPath={space?.dir || form.backupDir} issues={issues} verified={verified} busy={locked} onEdit={edit} />}
      {problem && !isRequirement(page) ? <Callout tone="danger" title={page === "review" ? "Nothing has been installed yet" : "This step couldn't finish"}>{problem.message}
        <div className="on-actions"><Button disabled={locked} onClick={() => void execute("checking", () => visit(pageRef.current, editingRef.current ? "edit" : "back"))}>Try again</Button><LogButton /></div>
      </Callout> : null}
    </SetupLayout>
  );
}
