import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import {
  AlertCircle,
  CheckCircle2,
  ExternalLink,
  FileText,
  Globe,
  KeyRound,
  Lock,
  Search,
  Unplug,
} from "lucide-react";

import { AppUpdateCard } from "@/components/app-update-card";
import { Screen, ScreenBody, ScreenHead } from "@/components/screen";
import { Spinner } from "@/components/spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { bridge } from "@/lib/bridge";
import { friendlyClientError, type FriendlyError } from "@/lib/client-errors";
import { cn } from "@/lib/utils";
import { useCare } from "@/state/care-store";

function displayHost(url: string): string {
  return url.replace(/^https?:\/\//, "").replace(/\/$/, "");
}

export function ClientScreen() {
  const { clientURL, clearRole, busy: updating } = useCare();
  const [address, setAddress] = useState(displayHost(clientURL));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<FriendlyError | null>(null);
  const [connected, setConnected] = useState(false);
  const [savedAddress, setSavedAddress] = useState(clientURL);
  const [removing, setRemoving] = useState(false);
  const [removeError, setRemoveError] = useState<FriendlyError | null>(null);
  const [leaving, setLeaving] = useState(false);
  const [removeApp, setRemoveApp] = useState(false);
  const [canRemoveApp, setCanRemoveApp] = useState(false);

  useEffect(() => {
    void bridge.CanRemoveApp().then(setCanRemoveApp, () => setCanRemoveApp(false));
  }, []);

  const saved = savedAddress !== "";
  const host = saved ? displayHost(savedAddress) : address.trim();

  const goBack = async () => {
    setLeaving(true);
    setError(null);
    await clearRole();
    setLeaving(false);
  };

  const removeAccess = async () => {
    setBusy(true);
    setRemoveError(null);
    try {
      await bridge.DisconnectClient();
    } catch (e) {
      setRemoveError(friendlyClientError(e));
      setBusy(false);
      return;
    }
    if (!removeApp) {
      window.location.reload();
      return;
    }
    try {
      await bridge.RemoveApp();
    } catch (e) {
      setSavedAddress("");
      setRemoveError(friendlyClientError(e));
    } finally {
      setBusy(false);
    }
  };

  const connect = async (event?: FormEvent) => {
    event?.preventDefault();
    if (busy || updating || !host) return;
    setBusy(true);
    setError(null);
    setConnected(false);
    try {
      await bridge.ConnectClient(host);
      setConnected(true);
    } catch (e) {
      setError(friendlyClientError(e));
    } finally {
      await bridge.GetState().then((state) => {
        setSavedAddress(state.client_url);
        if (state.client_url) setAddress(displayHost(state.client_url));
      }).catch(() => {});
      setBusy(false);
    }
  };

  return (
    <Screen>
      <ScreenHead
        className="mx-auto w-full max-w-2xl"
        kicker="CARE Desktop"
        title={saved ? "Your clinic" : "Connect to your clinic"}
        subtitle={saved
          ? "This computer is set up to open CARE from your clinic."
          : "Link this computer to your clinic so you can open CARE here."}
        onBack={saved ? undefined : () => void goBack()}
        backDisabled={busy || leaving || updating}
      />
      <ScreenBody className="mx-auto flex w-full max-w-2xl flex-col gap-4">
        <Card className="p-6">
          {saved ? (
            <SavedClinic host={host} busy={busy} disabled={updating} connected={connected} onOpen={() => void connect()} />
          ) : (
            <form onSubmit={(event) => void connect(event)} className="flex flex-col gap-5">
              <div className="flex flex-col gap-2">
                <Label htmlFor="clinic-address">Clinic address</Label>
                <div
                  className={cn(
                    "flex h-[46px] items-center gap-2.5 rounded-md border bg-white px-3 transition-colors focus-within:border-brand",
                    error ? "border-danger-line" : "border-line",
                  )}
                >
                  <Globe className="size-[18px] text-faint" strokeWidth={2} />
                  <Input
                    id="clinic-address"
                    className="h-full flex-1 border-0 px-0 text-[15px] shadow-none focus-visible:ring-0 disabled:bg-transparent"
                    value={address}
                    onChange={(event) => {
                      setAddress(event.target.value);
                      setError(null);
                    }}
                    placeholder="care.local"
                    autoFocus
                    autoCapitalize="none"
                    autoCorrect="off"
                    spellCheck={false}
                    disabled={busy || leaving || updating}
                    required
                  />
                </div>
                <p className="text-[12.5px] text-muted-foreground">
                  You can find this on the clinic's main computer, in CARE Desktop. It usually
                  looks like <span className="font-mono text-ink2">care.local</span>.
                </p>
              </div>

              {error ? <ErrorPanel error={error} /> : null}

              {busy ? (
                <Waiting host={host} />
              ) : (
                <Button type="submit" variant="primary" size="lg" disabled={leaving || updating || !host}>
                  Connect
                </Button>
              )}
            </form>
          )}
          {saved && error ? <div className="mt-5"><ErrorPanel error={error} /></div> : null}
        </Card>

        {saved ? null : <HowItWorks />}

        {saved ? (
          <Card className="flex flex-col gap-4 p-6 sm:flex-row sm:items-center">
            <div className="min-w-0 flex-1">
              <div className="text-[15px] font-semibold text-ink">Disconnect this computer</div>
              <p className="mt-1 text-[13px] text-muted-foreground">
                Moving to a different clinic or giving this computer to someone else?
                No patient or clinic data is deleted.
              </p>
            </div>
            <Button
              size="lg"
              className="border-danger-line text-danger-ink hover:border-danger hover:bg-danger-bg hover:text-danger-ink"
              disabled={busy || leaving || updating}
              onClick={() => {
                setRemoveError(null);
                setRemoving(true);
              }}
            >
              <Unplug className="size-[18px]" />
              Disconnect
            </Button>
          </Card>
        ) : null}

        <AppUpdateCard disabled={busy || leaving || removing} />

        <AlertDialog open={removing} onOpenChange={(open) => {
          if (!busy) setRemoving(open);
        }}>
          <AlertDialogContent>
            <AlertDialogTitle>Disconnect this computer?</AlertDialogTitle>
            <AlertDialogDescription>
              This computer will stop opening CARE from {host}. No patient or clinic
              data is deleted, and you can connect again at any time.
              Your computer may ask for your password.
            </AlertDialogDescription>
            {canRemoveApp ? (
              <label className="flex cursor-pointer items-center gap-2.5 text-[13px] text-ink2">
                <Checkbox
                  checked={removeApp}
                  disabled={busy}
                  onCheckedChange={(v) => setRemoveApp(v === true)}
                />
                <span>Also remove the CARE Desktop app from this computer.</span>
              </label>
            ) : null}
            {removeError ? <div className="mt-3"><ErrorPanel error={removeError} /></div> : null}
            <AlertDialogFooter>
              <AlertDialogCancel disabled={busy}>Cancel</AlertDialogCancel>
              <AlertDialogAction
                className={buttonVariants({ variant: "destructive" })}
                disabled={busy}
                onClick={(event) => {
                  event.preventDefault();
                  void removeAccess();
                }}
              >
                {busy ? <Spinner /> : null}
                {busy ? "Disconnecting…" : "Disconnect"}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </ScreenBody>
    </Screen>
  );
}

function SavedClinic({
  host,
  busy,
  disabled,
  connected,
  onOpen,
}: {
  host: string;
  busy: boolean;
  disabled: boolean;
  connected: boolean;
  onOpen: () => void;
}) {
  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center gap-3.5">
        <span className="flex size-11 flex-none items-center justify-center rounded-full bg-brand-bg text-brand-ink">
          <Globe className="size-5" strokeWidth={2} />
        </span>
        <div className="min-w-0">
          <div className="text-[12.5px] text-muted-foreground">Clinic address</div>
          <div className="truncate font-mono text-[16px] font-semibold text-ink">{host}</div>
        </div>
      </div>
      {connected ? (
        <div className="flex items-start gap-2.5 rounded-md border border-brand-line bg-brand-bg px-3.5 py-3 text-[13px] text-brand-ink">
          <CheckCircle2 className="mt-px size-[18px] flex-none" strokeWidth={2.2} />
          <span>
            <strong className="font-semibold">You're connected.</strong> CARE has opened in your
            browser. Next time, just click Open CARE.
          </span>
        </div>
      ) : null}
      {busy ? (
        <Waiting host={host} />
      ) : (
        <Button variant="primary" size="lg" disabled={disabled} onClick={onOpen}>
          <ExternalLink className="size-[18px]" />
          Open CARE
        </Button>
      )}
    </div>
  );
}

function Waiting({ host }: { host: string }) {
  return (
    <div role="status" className="flex items-start gap-3 rounded-md border border-line bg-background px-4 py-3.5">
      <Spinner className="mt-0.5 flex-none text-brand" />
      <div className="flex flex-col gap-1">
        <span className="text-[14px] font-semibold text-ink">Connecting to {host}…</span>
        <span className="text-[13px] text-muted-foreground">
          If your computer asks for your password, enter it and click OK or Yes.
          The window may be hidden behind this one.
        </span>
      </div>
    </div>
  );
}

function ErrorPanel({ error }: { error: FriendlyError }) {
  return (
    <div role="alert" className="rounded-md border border-danger-line bg-danger-tint px-4 py-3.5">
      <div className="flex items-start gap-2.5">
        <AlertCircle className="mt-px size-[18px] flex-none text-danger" strokeWidth={2.2} />
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-[14px] font-semibold text-danger-ink">{error.title}</span>
          <span className="text-[13px] text-ink2">{error.message}</span>
          {error.tips?.length ? (
            <ul className="mt-0.5 flex list-disc flex-col gap-1 pl-4 text-[13px] text-ink2">
              {error.tips.map((tip) => <li key={tip}>{tip}</li>)}
            </ul>
          ) : null}
          <button
            type="button"
            className="mt-1 inline-flex w-fit cursor-pointer items-center gap-1.5 text-[12px] font-semibold text-muted-foreground hover:text-ink2"
            onClick={() => void bridge.OpenLogFolder().catch(() => {})}
          >
            <FileText className="size-3.5" />
            Open log file for support
          </button>
        </div>
      </div>
    </div>
  );
}

function HowItWorks() {
  return (
    <div className="px-1">
      <div className="mb-3 text-xs font-bold tracking-[0.04em] text-muted-foreground uppercase">
        What happens next
      </div>
      <ol className="grid gap-3 sm:grid-cols-3">
        <Step icon={<Search className="size-4" />} n={1} title="We find your clinic">
          on the local network.
        </Step>
        <Step icon={<KeyRound className="size-4" />} n={2} title="You allow it">
          Your computer asks for its password once.
        </Step>
        <Step icon={<ExternalLink className="size-4" />} n={3} title="CARE opens">
          in your web browser.
        </Step>
      </ol>
      <p className="mt-4 flex items-center gap-2 text-[12.5px] text-muted-foreground">
        <Lock className="size-3.5 flex-none" />
        Only connect to an address your clinic gave you. This keeps your connection private.
      </p>
    </div>
  );
}

function Step({ icon, n, title, children }: { icon: ReactNode; n: number; title: string; children: ReactNode }) {
  return (
    <li className="flex items-start gap-2.5 rounded-lg border border-line bg-white p-3">
      <span className="flex size-7 flex-none items-center justify-center rounded-full bg-brand-bg text-brand-ink" aria-hidden>
        {icon}
      </span>
      <span className="text-[13px] leading-snug">
        <span className="sr-only">Step {n}: </span>
        <span className="block font-semibold text-ink">{title}</span>
        <span className="text-muted-foreground">{children}</span>
      </span>
    </li>
  );
}
