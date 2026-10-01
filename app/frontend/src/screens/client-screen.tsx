import { ArrowLeft, Check, Copy, ExternalLink, KeyRound, Plug, RefreshCw, Search, Sparkles, Unplug, WifiOff } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";

import {
  Callout, ClinicAddressInput, isClinicName, LogButton,
  OnboardingBrand, OnboardingUpdates, StatusBadge,
} from "@/components/onboarding";
import { Spinner } from "@/components/spinner";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button, buttonVariants } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "@/components/ui/sonner";
import { useAppUpdate } from "@/hooks/use-app-update";
import { bridge, onCareEvent } from "@/lib/bridge";
import { friendlyClientError, type FriendlyError } from "@/lib/client-errors";
import { errorText, normaliseHost } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useCare } from "@/state/care-store";
import type { ClientConnectPhase, ClientPreflight, ClientReachability, ClinicInfo } from "@/types";

type Phase = "entry" | "finding" | "found" | "connecting" | "saved";
type Operation = "find" | "connect" | "disconnect" | "back" | null;

const displayHost = (url: string) => url.replace(/^https?:\/\//i, "").replace(/\/$/, "");

export function ClientScreen() {
  const { clientURL, clearRole, restartSetup, busy: hostBusy, log } = useCare();
  const [address, setAddress] = useState(displayHost(clientURL).replace(/\.local$/i, "") || "care");
  const [savedAddress, setSavedAddress] = useState(clientURL);
  const [phase, setPhase] = useState<Phase>(clientURL ? "saved" : "entry");
  const [established, setEstablished] = useState(!!clientURL);
  const [justConnected, setJustConnected] = useState(false);
  const [found, setFound] = useState<ClinicInfo | null>(null);
  const [preflight, setPreflight] = useState<ClientPreflight | null>(null);
  const [progress, setProgress] = useState<ClientConnectPhase>("finding");
  const [error, setError] = useState<FriendlyError | null>(null);
  const [operation, setOperation] = useState<Operation>(null);
  const operationRef = useRef<Operation>(null);
  const [removing, setRemoving] = useState(false);
  const removingRef = useRef(false);
  const [removeApp, setRemoveApp] = useState(false);
  const [canRemoveApp, setCanRemoveApp] = useState(false);
  const [removeError, setRemoveError] = useState<FriendlyError | null>(null);
  const [removalOptionError, setRemovalOptionError] = useState("");
  const [reachability, setReachability] = useState<ClientReachability | null>(null);
  const [reachError, setReachError] = useState("");
  const [checkingReach, setCheckingReach] = useState(false);
  const [pollAttempt, setPollAttempt] = useState(0);
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const focusEntry = useRef(true);
  const update = useAppUpdate(operation !== null || removing, true, () => operationRef.current !== null || removingRef.current);
  const locked = operation !== null || update.active || hostBusy;
  const valid = isClinicName(address);
  const host = phase === "saved" ? displayHost(savedAddress) : found?.host || normaliseHost(address);

  const begin = (next: Operation) => {
    if (operationRef.current || update.isActive() || hostBusy) return false;
    operationRef.current = next;
    setOperation(next);
    return true;
  };
  const end = () => { operationRef.current = null; setOperation(null); };
  const showRemoval = (open: boolean) => { removingRef.current = open; setRemoving(open); };

  useEffect(() => onCareEvent("client-connect-progress", (next: ClientConnectPhase) => {
    if (operationRef.current === "connect") setProgress(next);
  }), []);

  useEffect(() => { focusEntry.current = phase === "entry"; }, [phase]);
  useEffect(() => {
    if (phase !== "entry" || locked || !focusEntry.current) return;
    inputRef.current?.focus();
    focusEntry.current = false;
  }, [phase, locked]);

  useEffect(() => {
    if (!savedAddress) return;
    let live = true;
    void bridge.CanRemoveApp().then(
      (value) => { if (live) setCanRemoveApp(value); },
      (e) => {
        log(`app removal option: ${errorText(e)}`);
        if (live) setRemovalOptionError("App removal couldn't be checked. You can still disconnect this computer.");
      },
    );
    return () => { live = false; };
  }, [savedAddress, log]);

  useEffect(() => {
    if (phase !== "saved" || !savedAddress || operation || update.active) return;
    let live = true;
    let timer = 0;
    const poll = async () => {
      setCheckingReach(true);
      try {
        const result = await bridge.ClientReachable();
        if (live) { setReachability(result); setReachError(""); }
      } catch (e) {
        log(`client reachability: ${errorText(e)}`);
        if (live) {
          setReachability(null);
          setReachError("Couldn't check the clinic connection. Try again, or share the log file with your support contact.");
        }
      } finally {
        if (live) {
          setCheckingReach(false);
          timer = window.setTimeout(() => void poll(), 15_000);
        }
      }
    };
    void poll();
    return () => { live = false; window.clearTimeout(timer); };
  }, [phase, savedAddress, operation, update.active, pollAttempt, log]);

  const readPreflight = async () => {
    const result = await bridge.ClientPreflight();
    setPreflight(result);
    if (result.unfinished_server_setup) {
      throw new Error("this computer has an unfinished clinic setup; remove it in Setup before connecting");
    }
    return result;
  };

  const findServer = async (event?: FormEvent) => {
    event?.preventDefault();
    if (!valid || !begin("find")) return;
    setError(null);
    setFound(null);
    setPhase("finding");
    try {
      await readPreflight();
      const result = await bridge.FindClinic(normaliseHost(address));
      setAddress(result.host.replace(/\.local$/i, ""));
      setFound(result);
      setPhase("found");
    } catch (e) {
      log(`find clinic: ${errorText(e)}`);
      setError(friendlyClientError(e));
      setPhase("entry");
    } finally { end(); }
  };

  const connect = async () => {
    if (!begin("connect")) return;
    const wasSaved = phase === "saved";
    const target = wasSaved ? savedAddress : found?.url || `https://${normaliseHost(address)}`;
    setError(null);
    setProgress("finding");
    setPhase("connecting");
    try {
      await readPreflight();
      await bridge.ConnectClient(target);
      setSavedAddress(target);
      setEstablished(true);
      setJustConnected(!wasSaved);
      setReachability(null);
      setPhase("saved");
    } catch (e) {
      log(`connect clinic: ${errorText(e)}`);
      setError(friendlyClientError(e));
      // Trust ownership can be saved before a permission prompt is declined.
      // Keep it visible and removable; a rejected promise is never "connected".
      try {
        const state = await bridge.GetState();
        setSavedAddress(state.client_url);
      } catch (stateError) {
        log(`read connection after failure: ${errorText(stateError)}`);
        setError({
          title: "The connection didn't finish",
          message: "CARE couldn't check what was saved. Try again, or reopen CARE Desktop before changing this connection.",
        });
      }
      setPhase(wasSaved ? "saved" : found ? "found" : "entry");
    } finally { end(); }
  };

  const goBack = async () => {
    if (!begin("back")) return;
    try { await clearRole(); } finally { end(); }
  };

  const disconnect = async () => {
    if (!begin("disconnect")) return;
    setRemoveError(null);
    let disconnected = false;
    try {
      await bridge.DisconnectClient();
      disconnected = true;
      if (removeApp) await bridge.RemoveApp();
      setSavedAddress("");
      setEstablished(false);
      setFound(null);
      setReachability(null);
      setPhase("entry");
      showRemoval(false);
      toast("Disconnected");
      await clearRole();
    } catch (e) {
      log(`disconnect clinic: ${errorText(e)}`);
      if (disconnected) {
        setSavedAddress("");
        setEstablished(false);
        setFound(null);
        setPhase("entry");
        showRemoval(false);
        setError({ title: "Disconnected, but the app couldn't be removed", message: "No clinic data was deleted. You can remove CARE Desktop using this computer's normal app settings." });
      } else {
        setRemoveError(friendlyClientError(e));
      }
    } finally { end(); }
  };

  const retryError = () => {
    if (error?.action === "setup") { if (begin("back")) restartSetup(); }
    else if (error?.action === "disconnect") showRemoval(true);
    else if (error?.action === "repair" || found || phase === "saved") void connect();
    else void findServer();
  };
  const errorAction = error?.action === "setup" ? "Open setup"
    : error?.action === "disconnect" ? "Disconnect"
    : error?.action === "repair" ? "Connect and fix" : "Try again";
  const secureProblem = reachability && !reachability.reachable && /certificate|verified/.test(reachability.detail);
  const title = phase === "saved" ? (justConnected ? "You're connected" : "Welcome back")
    : phase === "connecting" ? `Connecting to ${host}`
    : phase === "found" ? "We found your clinic's server"
    : error?.title === "We couldn't find the clinic" ? "We couldn't find that server"
    : "Find your clinic's server";
  const subtitle = phase === "saved" ? "This computer opens CARE from your clinic's server."
    : phase === "connecting" ? "Keep this window open. About a minute."
    : phase === "found" ? "Check the address, then connect this computer."
    : "Type the clinic address shown on the clinic's main computer.";

  return (
    <div className="onboarding onboarding-flow on-client">
      <aside className="on-left" aria-label="CARE Desktop">
        <OnboardingBrand />
        <div className="on-hero"><h2>Connect this computer to your clinic.</h2></div>
        <OnboardingUpdates controller={update} context="client" />
      </aside>
      <main className="on-client-body" aria-labelledby="client-title">
        <div className="on-client-nav">{!established ? <Button className="on-back" variant="ghost" disabled={locked} onClick={() => void goBack()}><ArrowLeft aria-hidden="true" />Back</Button> : null}</div>
        <div className="on-kicker">{phase === "saved" ? "Your clinic" : "Connect to an existing server"}</div>
        <h1 className="on-title" id="client-title">{title}</h1>
        <p className="on-subtitle">{subtitle}</p>
        <div className="on-client-content">
          {phase === "entry" || phase === "finding" ? (
            <form onSubmit={(e) => void findServer(e)}>
              <div className="on-field">
                <label htmlFor="clinic-address">Clinic address</label>
                <ClinicAddressInput id="clinic-address" ref={inputRef} value={address} disabled={locked}
                  invalid={!valid || !!error} aria-describedby="client-address-hint"
                  onValueChange={(value) => { setAddress(value); setError(null); setFound(null); }}
                />
                <p id="client-address-hint" className={cn("on-hint", !valid && "on-error")}>
                  {valid ? <>Just the name — for example <span className="on-mono">care</span> or <span className="on-mono">care-hospital</span>.</>
                    : "Type only the clinic's name — for example care — with nothing else before or after it."}
                </p>
              </div>
              {phase === "finding" ? (
                <div className="on-check-work" role="status"><Callout title={`Looking for ${host} on your network…`}>This usually takes a few seconds.</Callout></div>
              ) : !error ? (
                <div className="on-actions"><Button type="submit" variant="primary" className="on-primary" disabled={locked || !valid}><Search aria-hidden="true" />Find server</Button></div>
              ) : null}
            </form>
          ) : null}
          {phase === "found" && found && !error ? (
            <section className="on-card on-pad" aria-label="Server found">
              <div className="on-row">
                <span className="on-tile on-large on-round on-solid"><Check aria-hidden="true" /></span>
                <div className="on-grow"><div className="on-eyebrow">Server found</div><div className="on-found-host">{found.host}</div><p className="on-small">Answering on this network · Secure connection available</p></div>
              </div>
              <div className="on-divider" />
              <div className="on-eyebrow">What happens when you connect</div>
              <div className="on-stack" style={{ marginTop: 10 }}>
                <p className="on-hint"><KeyRound aria-hidden="true" />Your computer will ask for permission — password, fingerprint or PIN, depending on how you sign in. It may ask twice.</p>
                <p className="on-hint"><Sparkles aria-hidden="true" />Any old clinic address or clinic certificate left on this computer is removed automatically.</p>
                <p className="on-hint"><ExternalLink aria-hidden="true" />CARE opens in your web browser. Takes about a minute.</p>
              </div>
              <div className="on-actions">
                <Button variant="primary" className="on-primary" disabled={locked} onClick={() => void connect()}><Plug aria-hidden="true" />Connect</Button>
                <Button variant="ghost" disabled={locked} onClick={() => { setFound(null); setPhase("entry"); }}>Not this one</Button>
              </div>
            </section>
          ) : null}
          {phase === "connecting" ? <ConnectionProgress phase={progress} found={!!found || established} cleanup={!!preflight?.hosts_entry || !!preflight?.old_certificate} /> : null}
          {phase === "saved" ? (
            <section className="on-card on-pad" aria-label="Your clinic connection">
              <div className="on-row on-connection-heading">
                <span className={cn("on-tile on-large on-round", reachability?.reachable ? "on-solid" : reachability ? "on-warn" : "")} aria-hidden="true">
                  {!reachability ? <Spinner /> : reachability.reachable ? <Check /> : <WifiOff />}
                </span>
                <div className="on-grow"><div className="on-eyebrow">Connected to</div><div className="on-found-host">{host}</div></div>
                <StatusBadge tone={reachability?.reachable ? "ok" : reachability ? "warn" : ""}>{!reachability && !reachError ? "Checking" : reachability?.reachable ? "Connected" : "Can't reach it right now"}</StatusBadge>
              </div>
              <div className="on-divider" />
              {reachError ? <Callout title="Couldn't check the connection" tone="danger">{reachError}<div className="on-actions"><Button disabled={locked || checkingReach} onClick={() => setPollAttempt((n) => n + 1)}>Check again</Button><LogButton /></div></Callout>
                : reachability && !reachability.reachable ? (
                  <Callout tone="warn" title={secureProblem ? "We couldn't connect securely" : `This computer is set up for ${host}, but we can't find it right now`}>
                    {secureProblem ? <p>The clinic's security check didn't pass. Check this computer's date and time. If the clinic was set up again, disconnect here and connect again.</p>
                      : <ul><li>Are you on the same Wi-Fi or network as the clinic server?</li><li>Is CARE running on the clinic's server computer?</li></ul>}
                    <div className="on-actions"><Button disabled={locked || checkingReach} onClick={() => setPollAttempt((n) => n + 1)}><RefreshCw aria-hidden="true" />{checkingReach ? "Checking…" : "Check again"}</Button><span className="on-small">Checking automatically</span></div>
                  </Callout>
                ) : null}
              <div className="on-actions">
                <Button variant="primary" className="on-primary" disabled={locked || checkingReach || !reachability?.reachable} onClick={() => void connect()}><ExternalLink aria-hidden="true" />Open CARE</Button>
                <Button disabled={locked} onClick={() => {
                  setCopyError("");
                  setCopied(false);
                  void (async () => {
                    try { await navigator.clipboard.writeText(savedAddress); setCopied(true); }
                    catch (e) { log(`copy clinic address: ${errorText(e)}`); setCopyError("Couldn't copy the address. You can select it and copy it yourself."); }
                  })();
                }}><Copy aria-hidden="true" />{copied ? "Copied" : "Copy address"}</Button>
              </div>
              {copyError ? <p className="on-error" role="alert">{copyError}</p> : null}
              {reachability?.reachable ? <p className="on-small" style={{ marginTop: 16 }}>{justConnected ? `CARE has opened in your browser. Next time, open CARE Desktop and click Open CARE, or type ${host} in your browser.` : "Click Open CARE to start. · Server answering · checked just now"}</p> : null}
            </section>
          ) : null}
          {error ? (
            <Callout title={error.title} tone="danger">
              <p>{error.message}</p>{error.tips ? <ul>{error.tips.map((tip) => <li key={tip}>{tip}</li>)}</ul> : null}
              <div className="on-actions"><Button variant="primary" disabled={locked || (error.action === "repair" && !valid)} onClick={retryError}>{errorAction}</Button><LogButton label="Open log file for support" /></div>
            </Callout>
          ) : null}
          {savedAddress && phase !== "connecting" ? (
            <section className="on-card">
              <div className="on-data-row on-client-disconnect">
                <span className="on-tile"><Unplug aria-hidden="true" /></span>
                <div className="on-grow"><h3>Disconnect this computer</h3><p>Moving to a different clinic or handing this computer over? No patient or clinic data is deleted.</p></div>
                <Button className="on-disconnect" disabled={locked} onClick={() => { setRemoveError(null); showRemoval(true); }}>Disconnect</Button>
              </div>
            </section>
          ) : null}
        </div>
      </main>
      <AlertDialog open={removing} onOpenChange={(open) => { if (!operationRef.current) showRemoval(open); }}>
        <AlertDialogContent className="onboarding onboarding-dialog">
          <AlertDialogTitle>Disconnect this computer?</AlertDialogTitle>
          <AlertDialogDescription>This computer will stop opening CARE from {displayHost(savedAddress)}. No patient or clinic data is deleted, and you can connect again at any time. Your computer may ask for your password.</AlertDialogDescription>
          {canRemoveApp ? <label className="on-row"><Checkbox checked={removeApp} disabled={locked} onCheckedChange={(v) => setRemoveApp(v === true)} /><span>Also remove the CARE Desktop app from this computer</span></label> : null}
          {removalOptionError ? <p className="on-small">{removalOptionError}</p> : null}
          {removeError ? <Callout title={removeError.title} tone="danger">{removeError.message}<LogButton /></Callout> : null}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={locked}>Cancel</AlertDialogCancel>
            <AlertDialogAction className={buttonVariants({ variant: "destructive" })} disabled={locked} onClick={(e) => { e.preventDefault(); void disconnect(); }}>{operation === "disconnect" ? <Spinner /> : null}{operation === "disconnect" ? "Disconnecting…" : "Disconnect"}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function ConnectionProgress({ phase, found, cleanup }: { phase: ClientConnectPhase; found: boolean; cleanup: boolean }) {
  const index = phase === "finding" ? found ? 1 : 0 : phase === "connecting" ? 1 : phase === "checking" ? 3 : 4;
  const labels = [
    found || index > 0 ? "Found your clinic's server" : "Finding your clinic's server",
    cleanup ? "Removing an old clinic address and certificate" : "Checking for old clinic settings",
    "Setting up the secure connection", "Checking the connection", "Opening CARE",
  ];
  return (
    <>
      <div className="on-card on-pad" role="status" aria-live="polite">
        <ol className="on-steps">{labels.map((label, i) => (
          <li key={i} className={cn(i < index ? "on-done" : i === index ? "on-current" : "")}>
            <span className="on-step-dot" aria-hidden="true">{i < index ? <Check /> : i === index ? <Spinner /> : i + 1}</span>
            <div><strong>{label}</strong>{i === index && i === 1 ? <p>Your computer may ask for permission — password, fingerprint or PIN. The window may appear behind this one.</p> : i === index && i === 3 ? <p>Making sure CARE opens safely</p> : null}</div>
          </li>
        ))}</ol>
      </div>
      {index === 1 ? <Callout title="Your computer may ask for permission" tone="info">If nothing appears, look for a small window asking for your password, fingerprint or PIN. It may ask a second time for the certificate.</Callout> : null}
    </>
  );
}
