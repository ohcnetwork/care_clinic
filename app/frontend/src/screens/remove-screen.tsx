import { Trash2, TriangleAlert } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { Screen, ScreenBody, ScreenHead } from "@/components/screen";
import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Input } from "@/components/ui/input";
import { bridge } from "@/lib/bridge";
import { AdminGate, UninstallPanel } from "@/screens/panel/advanced-tab";
import { AdvancedError, AdvancedNotice, advancedProblem, type AdvancedProblem } from "@/screens/panel/advanced-ui";
import { useCare } from "@/state/care-store";

type Setup = "loading" | "server" | "leftovers" | "client";

export function RemoveScreen() {
  const { busy, busyLabel, clientURL, operationError } = useCare();
  const [setup, setSetup] = useState<Setup>("loading");
  const [adminPassword, setAdminPassword] = useState<string | null>(null);
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState<AdvancedProblem | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const [cleaned, setCleaned] = useState(false);
  const pending = useRef(false);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const version = useRef(0);

  const readSetup = useCallback(async () => {
    const read = ++version.current;
    setProblem(null);
    try {
      const state = await bridge.GetState();
      if (read === version.current) setSetup(state.role === "client" ? "client" : state.setup_done ? "server" : "leftovers");
    } catch (cause) {
      if (read === version.current) setProblem(advancedProblem(cause, "This installation couldn't be checked",
        "Nothing has been removed. Check again before continuing."));
    }
  }, []);
  useEffect(() => {
    void readSetup();
    return () => { version.current++; };
  }, [readSetup]);

  const attempt = async (step: () => Promise<boolean>) => {
    if (pending.current || busy) return;
    pending.current = true;
    setWorking(true);
    setProblem(null);
    try {
      if (!cleaned && !(await step())) {
        setProblem({ title: "Some setup files still need to be removed",
          message: "CARE Clinic has been kept so removal can be retried. Open the log file for support." });
        return;
      }
      setCleaned(true);
      setConfirming(false);
      await bridge.ExitUninstall();
    } catch (cause) {
      setProblem(advancedProblem(cause, "Removal couldn't finish",
        "CARE Clinic has been kept. Try again, or open the log file for support."));
    } finally {
      setConfirmation("");
      pending.current = false;
      setWorking(false);
    }
  };

  const disconnect = () =>
    attempt(async () => {
      await bridge.DisconnectClient();
      return true;
    });

  const removeLeftovers = () =>
    attempt(async () => {
      const after = await bridge.PurgeResidue(true);
      return after.clean;
    });

  const locked = busy || working;
  const close = () => {
    if (pending.current) return;
    setConfirming(false);
    setConfirmation("");
    setProblem(null);
  };
  const keep = async () => {
    if (pending.current || busy) return;
    pending.current = true;
    setWorking(true);
    setProblem(null);
    try {
      await bridge.ExitUninstall();
    } catch (cause) {
      setProblem(advancedProblem(cause, "The uninstaller couldn't be closed", "Try again when CARE Clinic is idle."));
    } finally {
      pending.current = false;
      setWorking(false);
    }
  };

  return (
    <Screen className="care-removal">
      <ScreenHead
        kicker="Uninstall"
        title="Remove CARE from this computer first"
        subtitle="The Windows uninstaller removes the app only after its clinic setup is gone."
      />
      <ScreenBody className="flex flex-col gap-4">
        {setup === "loading" && !problem ? <p className="advanced-busy" role="status"><Spinner />Checking this installation…</p> : null}
        {cleaned ? <AdvancedNotice title="The clinic setup has been removed" tone="success">
          The system uninstaller can now remove the CARE Clinic app.
        </AdvancedNotice> : null}
        {setup === "server" ? (
          adminPassword === null ? (
            <AdminGate onUnlock={setAdminPassword} />
          ) : (
            <section className="advanced-card advanced-card-pad flex flex-col gap-3">
              <h2 className="advanced-card-title">Uninstall the clinic</h2>
              <p className="advanced-card-description">
                This deletes the clinic and all patient data on this computer. The app is
                removed afterwards.
              </p>
              <UninstallPanel adminPassword={adminPassword} />
            </section>
          )
        ) : null}

        {setup === "client" ? (
          <section className="advanced-card advanced-card-pad flex flex-col gap-3">
            <h2 className="advanced-card-title">Disconnect this computer</h2>
            <p className="advanced-card-description">
              This computer stops opening CARE{clientURL ? ` from ${clientURL}` : ""}. No
              patient or clinic data is deleted. Your computer may ask for your password.
            </p>
            <Button
              variant="destructive"
              className="self-start"
              disabled={locked}
              onClick={() => void disconnect()}
            >
              {working ? <Spinner /> : null}
              {cleaned ? "Continue uninstalling the app" : "Disconnect and uninstall"}
            </Button>
          </section>
        ) : null}

        {setup === "leftovers" ? (
          <section className="advanced-card advanced-card-pad flex flex-col gap-3">
            <h2 className="advanced-card-title">Remove the unfinished setup</h2>
            <p className="advanced-card-description">
              This computer has files and settings from a clinic setup that did not finish.
              Backups are kept. Keep your separately saved backup recovery file to restore them.
            </p>
            <Button
              variant="destructive"
              className="self-start"
              disabled={locked}
              onClick={() => {
                if (cleaned) void removeLeftovers();
                else { setConfirmation(""); setProblem(null); setConfirming(true); }
              }}
            >
              {working ? <Spinner /> : null}
              {cleaned ? "Continue uninstalling the app" : "Remove unfinished setup…"}
            </Button>
          </section>
        ) : null}

        {busy ? (
          <div className="flex items-center gap-2.5 text-[13px] text-muted-foreground">
            <Spinner />
            {busyLabel || "Working"}…
          </div>
        ) : null}
        {!confirming ? <AdvancedError problem={problem ?? operationError} /> : null}
        {setup === "loading" && problem ? <Button type="button" className="self-start" onClick={() => void readSetup()}>Check installation again</Button> : null}

        {!cleaned ? <Button className="self-start" disabled={locked} onClick={() => void keep()}>
          Keep CARE Clinic
        </Button> : null}
      </ScreenBody>
      <AlertDialog open={confirming} onOpenChange={(next) => { if (!next) close(); }}>
        <AlertDialogContent className="advanced-dialog advanced-dialog-narrow"
          onOpenAutoFocus={(event) => { event.preventDefault(); cancelRef.current?.focus(); }}
          onEscapeKeyDown={(event) => { if (working) event.preventDefault(); }}>
          <AlertDialogTitle>Remove the unfinished clinic setup?</AlertDialogTitle>
          <AlertDialogDescription>This removes the old clinic files and settings. Backups and your separately saved recovery file are kept.</AlertDialogDescription>
          <div className="advanced-dialog-body">
            <AdvancedError problem={problem} />
            <div className="advanced-confirm"><TriangleAlert aria-hidden="true" /><div className="advanced-field">
              <label htmlFor="unfinished-delete">Type DELETE to confirm</label>
              <Input id="unfinished-delete" value={confirmation} onChange={(event) => setConfirmation(event.target.value)}
                autoComplete="off" spellCheck={false} disabled={locked} placeholder="DELETE" />
            </div></div>
          </div>
          <div className="advanced-dialog-foot">
            <Button type="button" ref={cancelRef} disabled={working} onClick={close}>Cancel</Button>
            <Button type="button" variant="destructive" disabled={locked || confirmation !== "DELETE"}
              onClick={() => void removeLeftovers()}><Trash2 aria-hidden="true" className="size-4" />Remove setup</Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </Screen>
  );
}
