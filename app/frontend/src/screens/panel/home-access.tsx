import { Copy, ExternalLink, House, Power, QrCode, RefreshCw, Share2, Sun } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { useCallback, useEffect, useRef, useState } from "react";

import {
  AlertDialog, AlertDialogAction, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/sonner";
import { bridge } from "@/lib/bridge";
import { useCare } from "@/state/care-store";
import type { HomeAccess } from "@/types";
import { PanelBadge, usePanelTask } from "./panel-ui";
import { usePanelUpdateLock } from "./panel-update-lock";

const DOWNLOAD_URL = "https://tailscale.com/download";
const DNS_SETTINGS_URL = "https://login.tailscale.com/admin/dns";
const MACHINES_URL = "https://login.tailscale.com/admin/machines";

type View = {
  badge: string;
  tone: "ok" | "neutral" | "danger";
  title: string;
  detail: string;
};

function view(status: HomeAccess | null, error: string): View {
  if (error) return { badge: "Couldn't check", tone: "danger", title: "Couldn't check home access", detail: error };
  if (!status) return { badge: "Checking", tone: "neutral", title: "Checking home access", detail: "" };
  if (!status.installed) return {
    badge: "Not set up", tone: "neutral", title: "Let doctors use CARE from home",
    detail: "Install Tailscale on this computer and sign in with the clinic's Tailscale account. Only people you share this computer with can reach CARE.",
  };
  if (!status.running) return { badge: "Not connected", tone: "neutral", title: "Tailscale is not connected", detail: status.detail };
  if (status.unknown) return { badge: "Couldn't check", tone: "danger", title: "Couldn't check home access", detail: status.detail };
  if (status.conflict) return { badge: "Needs attention", tone: "danger", title: "Tailscale is busy with something else", detail: status.detail };
  if (!status.https) return {
    badge: "One more step", tone: "neutral", title: "Turn on HTTPS in Tailscale",
    detail: "In the Tailscale admin console, open DNS and turn on HTTPS Certificates. Then check again.",
  };
  if (!status.enabled) return {
    badge: "Off", tone: "neutral", title: "Home access is off",
    detail: "Turn it on to let doctors you share this computer with open CARE from home.",
  };
  if (!status.clinic_ready) return {
    badge: "On", tone: "ok", title: "Home access is on",
    detail: "It works as soon as the clinic is running.",
  };
  return {
    badge: "On", tone: "ok", title: "Home access is on",
    detail: "Doctors you share this computer with can open CARE at this address.",
  };
}

export function HomeAccessCard({ locked }: { locked: boolean }) {
  const care = useCare();
  const updateLock = usePanelUpdateLock();
  const check = usePanelTask();
  const change = usePanelTask();
  const open = usePanelTask();
  const [status, setStatus] = useState<HomeAccess | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const mounted = useRef(true);
  const runCheck = useRef(check.run);
  runCheck.current = check.run;

  const refresh = useCallback(async () => {
    const next = await bridge.HomeAccessStatus();
    if (mounted.current) setStatus(next);
  }, []);

  useEffect(() => {
    mounted.current = true;
    void runCheck.current(refresh, "Couldn't check Tailscale on this computer.");
    return () => { mounted.current = false; };
  }, [refresh, care.system]);

  const busy = locked || updateLock.active || change.working;
  const current = view(status, check.error);
  const address = status?.url ?? "";

  const openURL = (url: string) => void open.run(() => bridge.OpenURL(url), "Couldn't open the browser. Try again.");

  const enable = () => void change.run(async () => {
    const restart = await bridge.EnableHomeAccess();
    await refresh();
    if (restart && care.system !== "stopped") {
      toast("Restarting the clinic to finish turning on home access");
      void care.runAction("restart");
    } else if (restart) {
      toast("Home access is on. It starts working when the clinic starts.");
    } else {
      toast("Home access is on");
    }
  }, "Couldn't turn on home access. Check Tailscale, then try again.");

  const disable = () => void change.run(async () => {
    await bridge.DisableHomeAccess();
    await refresh();
    toast("Home access is off");
  }, "Couldn't turn off home access. Try again, or turn it off in Tailscale.");

  return <section className="panel-card panel-home-access" aria-label="Home access">
    <div className="panel-row panel-between">
      <h2 className="panel-eyebrow"><House aria-hidden="true" />Home access</h2>
      <PanelBadge tone={current.tone} working={check.working || change.working}>{current.badge}</PanelBadge>
    </div>
    <h2>{current.title}</h2>
    {current.detail ? <p className="panel-small">{current.detail}</p> : null}
    {status?.enabled && address ? <div className="panel-home-address">{address}</div> : null}
    {status?.enabled ? <p className="panel-qr-hint"><Sun aria-hidden="true" />Keep this computer on and awake. Home access stops while it sleeps.</p> : null}
    <div className="panel-actions">
      {status && !status.installed ? <Button variant="primary" disabled={open.working} onClick={() => openURL(DOWNLOAD_URL)}>
        <ExternalLink aria-hidden="true" />Get Tailscale
      </Button> : null}
      {status?.running && !status.unknown && !status.https && !status.conflict ? <Button variant="primary" disabled={open.working} onClick={() => openURL(DNS_SETTINGS_URL)}>
        <ExternalLink aria-hidden="true" />Open Tailscale settings
      </Button> : null}
      {status?.running && status.https && !status.unknown && !status.conflict && !status.enabled ? <Button variant="primary" disabled={busy} onClick={enable}>
        <Power aria-hidden="true" />Turn on home access
      </Button> : null}
      {status?.enabled ? <>
        <Button variant="primary" disabled={!address} onClick={() => setDialogOpen(true)}>
          <QrCode aria-hidden="true" />Doctor instructions
        </Button>
        <Button disabled={open.working} onClick={() => openURL(status.ip ? `${MACHINES_URL}/${status.ip}` : MACHINES_URL)}>
          <Share2 aria-hidden="true" />Share with a doctor
        </Button>
        <Button variant="ghost" disabled={busy} onClick={disable}>Turn off</Button>
      </> : null}
      {!status?.enabled || check.error ? <Button variant="ghost" disabled={check.working || change.working}
        onClick={() => void check.run(refresh, "Couldn't check Tailscale on this computer.")}>
        <RefreshCw aria-hidden="true" />Check again
      </Button> : null}
    </div>
    {status?.enabled ? <p className="panel-small panel-home-share">
      To add a doctor, choose <strong>Share with a doctor</strong>, then <strong>Share</strong> in Tailscale and enter their email.
    </p> : null}
    {change.error || open.error ? <p className="panel-inline-error" role="alert">{change.error || open.error}</p> : null}
    {dialogOpen && address ? <DoctorDialog address={address} onClose={() => setDialogOpen(false)} /> : null}
  </section>;
}

function DoctorDialog({ address, onClose }: { address: string; onClose: () => void }) {
  const task = usePanelTask();
  const opener = useRef(document.activeElement instanceof HTMLElement ? document.activeElement : null);
  const copy = async () => {
    if (await task.run(() => navigator.clipboard.writeText(address),
      "Couldn't copy the address. Select and copy the address above instead.")) toast("Address copied");
  };
  return <AlertDialog open onOpenChange={(next) => { if (!next) onClose(); }}>
    <AlertDialogContent className="panel-dialog" onCloseAutoFocus={(event) => {
      event.preventDefault();
      opener.current?.focus();
    }}>
      <AlertDialogTitle>Use CARE from home</AlertDialogTitle>
      <AlertDialogDescription className="panel-dialog-description">
        Show this to the doctor, or send them the address. They only need to do this once on each phone or laptop.
      </AlertDialogDescription>
      <div className="panel-qr-row">
        <div className="panel-qr">
          <QRCodeSVG value={address} level="M" size={148} marginSize={4}
            role="img" aria-label={`QR code for ${address}`} />
        </div>
        <div className="panel-grow">
          <div className="panel-eyebrow">CARE address</div>
          <span className="panel-qr-address">{address}</span>
          <ol className="panel-qr-steps">
            <li>Install the Tailscale app and sign in with your own account.</li>
            <li>Open the email from the clinic and accept the shared computer.</li>
            <li>Turn Tailscale on, then open this address and bookmark it.</li>
          </ol>
          <p className="panel-qr-hint"><House aria-hidden="true" />Works from home or anywhere with internet, while Tailscale is on.</p>
        </div>
      </div>
      {task.error ? <p className="panel-inline-error" role="alert">{task.error}</p> : null}
      <AlertDialogFooter className="panel-dialog-foot">
        <Button disabled={task.working} onClick={() => void copy()}><Copy aria-hidden="true" />Copy address</Button>
        <span className="panel-grow" />
        <AlertDialogAction onClick={onClose}>Done</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}
