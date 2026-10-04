import { Check, Download, HardDrive, Monitor, Network, RefreshCw, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";

import { Callout, LogButton, StatusBadge } from "@/components/onboarding";
import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { bridge } from "@/lib/bridge";
import { diskSize, errorText } from "@/lib/format";
import { downloadProblem, type PrerequisiteProblem } from "@/lib/prerequisite-errors";
import { useCare } from "@/state/care-store";
import type { DownloadInfo, PrereqDownloadProgress, ResidueReport } from "@/types";
import { INSTALL_MIN_FREE, SOFTWARE_DESCRIPTIONS, type RequirementPage } from "./setup-model";
import type { SetupChecks } from "./use-setup-checks";

export type RequirementAction = "windows" | "software" | "docker" | "git" | "cleanup" | "network";

export function RequirementStep({ page, checks, busy, tool, download, actionError, actionNote, cleanupBefore, onAction, onCheck, onRestart }: {
  page: RequirementPage;
  checks: SetupChecks;
  busy: boolean;
  tool: string;
  download: PrereqDownloadProgress | null;
  actionError: PrerequisiteProblem | null;
  actionNote: string;
  cleanupBefore: ResidueReport | null;
  onAction: (action: RequirementAction) => void;
  onCheck: () => void;
  onRestart: () => void;
}) {
  const { platform, log } = useCare();
  const engine = platform === "linux" ? "Docker" : "Rancher Desktop";
  const [downloadInfo, setDownloadInfo] = useState<DownloadInfo | null>(null);
  const [sizeError, setSizeError] = useState<PrerequisiteProblem | null>(null);
  const [sizeAttempt, setSizeAttempt] = useState(0);
  const previewNeeded = page === "software" && checks.software.value?.dockerPlan?.download_preview;
  useEffect(() => {
    if (!previewNeeded) return;
    let live = true;
    setDownloadInfo(null);
    setSizeError(null);
    void bridge.RancherDownloadInfo().then(
      (value) => { if (live) setDownloadInfo(value); },
      (e) => {
        log(`download size: ${errorText(e)}`);
        if (live) setSizeError(downloadProblem(e, true) ?? { message: "Couldn't check the download size. Try again before downloading." });
      },
    );
    return () => { live = false; };
  }, [previewNeeded, sizeAttempt, log]);
  const check = checks[page];
  const ready = check.state === "ready";
  const checking = check.state === "waiting" || check.state === "checking";
  const disk = checks.space.value;
  const settingsDrive = !!disk && /settings/.test(disk.how);
  const footer = (label = "Check again") => <Button disabled={busy} onClick={onCheck}><RefreshCw aria-hidden="true" />{label}</Button>;

  if (checking && !check.value) {
    return <div className="on-card on-pad" role="status"><div className="on-card-head"><span className="on-tile on-large"><Spinner /></span><div className="on-grow"><h2>{page === "space" ? "Measuring the space on this computer" : "Checking this step"}</h2><p>This takes a moment.</p></div><StatusBadge>Checking</StatusBadge></div></div>;
  }

  return (
    <>
      {page === "space" ? (
        <div className="on-card on-pad">
          <div className="on-card-head"><span className={`on-tile on-large${ready ? "" : " on-bad"}`}><HardDrive aria-hidden="true" /></span><div className="on-grow"><h2>Free space on this computer</h2><p>CARE Clinic needs at least {diskSize(disk?.need && !settingsDrive ? disk.need : INSTALL_MIN_FREE)} for the clinic software and records.</p></div><StatusBadge tone={ready ? "ok" : checking ? "" : "bad"}>{checking ? "Checking" : ready ? "Enough space" : check.error ? "Couldn't check" : "Free up space"}</StatusBadge></div>
          <div className="on-check-work">
            {disk?.need ? <>
              <dl className={`on-space-summary${ready ? " on-space-sufficient" : checking ? "" : " on-space-insufficient"}`}>
                <div><dt>{settingsDrive ? "Settings drive needs" : "Space needed"}</dt><dd>{diskSize(disk.need)}</dd></div>
                <div><dt>{settingsDrive ? "Available on settings drive" : "Available space"}</dt><dd>{diskSize(disk.free)}</dd></div>
              </dl>
              {!ready && !checking && !check.error ? <Callout tone="danger" title={settingsDrive ? "The drive that holds CARE's settings is nearly full." : "There isn't enough space yet."}>
                Empty the Trash or Recycle Bin, then delete large downloads or programs you no longer use. Click Check again when you're done.
              </Callout> : null}
            </> : <p>The available space couldn't be measured. Check again before continuing.</p>}
            <div className="on-actions">{footer()}</div>
          </div>
        </div>
      ) : page === "windows" ? (
        <div className="on-card on-pad">
          <div className="on-card-head"><span className="on-tile on-large"><Monitor aria-hidden="true" /></span><div className="on-grow"><h2>{checks.windows.value?.restart.needed ? "WSL 2 installed — Windows needs to restart" : ready ? "Windows is ready" : "WSL 2 needs to be installed or turned on"}</h2><p>Windows Subsystem for Linux — {engine} runs the clinic inside it.</p></div><StatusBadge tone={ready ? "ok" : "bad"}>{ready ? "Available" : checks.windows.value?.restart.needed ? "Restart" : "Needs setup"}</StatusBadge></div>
          {!ready ? <div className="on-check-work"><Callout title={checks.windows.value?.restart.needed ? "Save anything you have open before restarting." : "CARE will install WSL 2 for you."}>
            It switches on two Windows features — Virtual Machine Platform and Windows Subsystem for Linux — then installs WSL 2. Windows will ask for permission and usually needs to restart afterwards.
            <div className="on-actions">{checks.windows.value?.restart.needed ? <Button variant="primary" disabled={busy} onClick={onRestart}>Restart now</Button>
              : checks.windows.value?.status.fixable ? <Button variant="primary" disabled={busy} onClick={() => onAction("windows")}>{tool ? <Spinner /> : null}{tool ? "Installing WSL 2…" : "Install WSL 2"}</Button> : null}{footer()}</div>
          </Callout></div> : null}
          <p className="on-small" style={{ marginTop: 18 }}>The other Windows change comes at Network profile — switching this network from Public to Private, so staff devices can reach the clinic.</p>
        </div>
      ) : page === "software" ? (
        <>
          <div className="on-card">
            {(["docker", "git"] as const).map((id) => {
              const software = checks.software.value;
              const status = software?.[id];
              const name = id === "docker" ? engine : "Git";
              const active = tool === id;
              const plan = id === "docker" ? software?.dockerPlan : software?.gitPlan;
              const tone = status?.ok ? "ok" : tool ? "" : "bad";
              return <div className="on-data-row" key={id}>
                <span className={tone === "bad" ? "on-tile on-bad" : "on-tile"}>{active ? <Spinner /> : status?.ok ? <Check /> : <Download />}</span>
                <div className="on-grow"><strong>{name}</strong>
                  <p>{SOFTWARE_DESCRIPTIONS[id]}</p>
                  {active ? <p role="status">{download?.phase === "downloading" ? `${diskSize(download.done)}${download.total > 0 ? ` of ${diskSize(download.total)}` : ""} downloaded`
                    : download?.phase === "verifying" ? "Checking the downloaded file"
                    : download?.phase === "complete" ? "Downloaded — installing now"
                    : plan?.action === "open" ? "Starting — this can take a minute" : "Preparing the installation"}</p> : null}
                  {active && download ? <Progress className="on-progress" aria-label={`${name} download progress`} value={download.total > 0 ? Math.max(0, Math.min(100, download.done / download.total * 100)) : null} /> : null}
                </div>
                <StatusBadge tone={tone}>{active ? "Working" : status?.ok ? "Available" : tool ? "Waiting" : "Needs setup"}</StatusBadge>
              </div>;
            })}
          </div>
          {!ready && !tool ? <Callout title={checks.software.value?.dockerPlan?.action === "open" ? `${engine} isn't running, so the clinic can't be installed` : "Install the software CARE needs"}>
            {checks.software.value?.dockerPlan?.action === "open" ? "Starting it takes about a minute."
              : <>Keep this computer connected to the internet. Your computer may ask for permission. {previewNeeded ? downloadInfo ? `${engine} download: ${diskSize(downloadInfo.size)}.` : sizeError ? "The download size couldn't be checked." : "Checking the download size…" : ""}</>}
            {platform === "darwin" && !checks.software.value?.git.ok ? <p style={{ marginTop: 8 }}>For Git, your Mac will show its own window — choose Install. Once it's done, choose Continue here.</p> : null}
            <div className="on-actions">
              <Button variant="primary" disabled={busy || !!previewNeeded && !downloadInfo} onClick={() => onAction("software")}>
                {checks.software.value?.dockerPlan?.action === "open" ? `Start ${engine}` : checks.software.value?.dockerPlan?.action === "manual" ? `Get ${engine}` : checks.software.value?.docker.ok ? "Install Git" : "Install them"}
              </Button>
              {footer()}
            </div>
          </Callout> : null}
          {tool ? <Callout tone="info" title="Your computer may ask for permission">Look for a small window asking for a password, fingerprint or PIN. It can open behind this one. Keep CARE Clinic open.</Callout> : null}
          {sizeError ? <Callout tone="danger" title={sizeError.title ?? "Couldn't check the download"}>{sizeError.message}<div className="on-actions"><Button disabled={busy} onClick={() => setSizeAttempt((n) => n + 1)}>Try again</Button><LogButton /></div></Callout> : null}
        </>
      ) : page === "cleanup" ? (
        <div className="on-card on-pad">
          <div className="on-card-head"><span className="on-tile on-large"><Trash2 aria-hidden="true" /></span><div className="on-grow"><h2>{ready ? "Nothing from the earlier setup is left" : tool ? "Removing the earlier setup" : "Still on this computer"}</h2></div>{tool ? <Spinner /> : null}</div>
          <ul className="on-leftovers">{(cleanupBefore?.traces ?? checks.cleanup.value?.traces ?? []).map((trace) => {
            const removed = cleanupBefore && checks.cleanup.state !== "failed" && checks.cleanup.value && !(checks.cleanup.value.traces ?? []).some((item) => item.id === trace.id);
            return <li key={trace.id}>{trace.label}{removed ? <span className="on-success"> — Removed</span> : cleanupBefore && !tool ? " — Still here" : ""}</li>;
          })}</ul>
          {!ready ? <><p>This deletes the earlier clinic's database and files from this computer and can't be undone. Backups that clinic made are kept.</p>
            <p className="on-small" style={{ marginTop: 8 }}>Any recovery materials saved during this setup will need to be saved again after removal. Your computer may ask for a password, fingerprint or PIN.</p>
            <div className="on-actions"><Button variant="destructive" disabled={busy} onClick={() => onAction("cleanup")}>{tool ? <Spinner /> : <Trash2 aria-hidden="true" />}{tool ? "Removing…" : cleanupBefore ? "Try again" : "Remove it all"}</Button></div>
          </> : <p className="on-success">The check found no leftovers. Your backups have been kept.</p>}
        </div>
      ) : (
        <div className="on-card on-pad">
          <div className="on-card-head"><span className="on-tile on-large"><Network aria-hidden="true" /></span><div className="on-grow"><h2>{ready ? "This network is ready" : checks.network.value?.fixable ? "This network is set to Public" : "We couldn't read this network's profile just now"}</h2><p>{ready ? "Staff devices on this network can reach the clinic." : "Windows blocks incoming connections on a Public network, so devices can't reach the clinic."}</p></div><StatusBadge tone={ready ? "ok" : "bad"}>{ready ? "Available" : "Needs setup"}</StatusBadge></div>
          {!ready ? <div className="on-check-work"><Callout title={checks.network.value?.fixable ? "CARE will switch this network to Private." : "Connect this computer to the clinic's network, then check again."}>
            On a Private network Windows lets devices on the same Wi-Fi reach this computer. It applies to this network only — networks you join elsewhere are untouched. Windows will ask for permission.
            <div className="on-actions">{checks.network.value?.fixable ? <Button variant="primary" disabled={busy} onClick={() => onAction("network")}>{tool ? <Spinner /> : null}{tool ? "Changing…" : "Set to Private"}</Button> : null}{footer()}</div>
          </Callout></div> : null}
        </div>
      )}
      {check.error || actionError ? <Callout tone="danger" title={actionError?.title ?? "This step couldn't finish"}>{actionError?.message || check.error}<div className="on-actions"><Button disabled={busy} onClick={() => actionError && page !== "space" ? onAction(page) : onCheck()}>Try again</Button><LogButton /></div></Callout> : null}
      {actionNote && !actionError ? <Callout>{actionNote}{!busy ? <div className="on-actions">{footer()}</div> : null}</Callout> : null}
    </>
  );
}
