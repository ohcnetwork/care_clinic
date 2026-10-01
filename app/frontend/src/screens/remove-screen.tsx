import { useEffect, useState } from "react";

import { Screen, ScreenBody, ScreenHead } from "@/components/screen";
import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { bridge } from "@/lib/bridge";
import { errorText, firstLine } from "@/lib/format";
import { AdminGate, UninstallPanel } from "@/screens/panel/advanced-tab";
import { useCare } from "@/state/care-store";

type Setup = "loading" | "server" | "leftovers" | "client";

export function RemoveScreen() {
  const { busy, busyLabel, clientURL } = useCare();
  const [setup, setSetup] = useState<Setup>("loading");
  const [adminPassword, setAdminPassword] = useState<string | null>(null);
  const [working, setWorking] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    void bridge.GetState().then(
      (state) =>
        setSetup(
          state.role === "client" ? "client" : state.setup_done ? "server" : "leftovers",
        ),
      (e) => setError(firstLine(errorText(e))),
    );
  }, []);

  const attempt = async (step: () => Promise<boolean>) => {
    setWorking(true);
    setError("");
    try {
      if (await step()) {
        await bridge.ExitUninstall();
        return;
      }
    } catch (e) {
      setError(firstLine(errorText(e)));
    }
    setWorking(false);
  };

  const disconnect = () =>
    attempt(async () => {
      await bridge.DisconnectClient();
      return true;
    });

  const removeLeftovers = () =>
    attempt(async () => {
      await bridge.PurgeResidue();
      return (await bridge.ScanResidue()).clean;
    });

  const locked = busy || working;

  return (
    <Screen>
      <ScreenHead
        kicker="Uninstall"
        title="Remove CARE from this computer first"
        subtitle="The Windows uninstaller removes the app only after its clinic setup is gone."
      />
      <ScreenBody className="flex flex-col gap-4">
        {setup === "server" ? (
          adminPassword === null ? (
            <AdminGate onUnlock={setAdminPassword} />
          ) : (
            <Card className="flex flex-col gap-3 p-6">
              <CardTitle>Uninstall the clinic</CardTitle>
              <CardDescription>
                This deletes the clinic and all patient data on this computer. The app is
                removed afterwards.
              </CardDescription>
              <UninstallPanel adminPassword={adminPassword} />
            </Card>
          )
        ) : null}

        {setup === "client" ? (
          <Card className="flex flex-col gap-3 p-6">
            <CardTitle>Disconnect this computer</CardTitle>
            <CardDescription>
              This computer stops opening CARE{clientURL ? ` from ${clientURL}` : ""}. No
              patient or clinic data is deleted. Your computer may ask for your password.
            </CardDescription>
            <Button
              variant="destructive"
              className="self-start"
              disabled={locked}
              onClick={() => void disconnect()}
            >
              {working ? <Spinner /> : null}
              Disconnect and uninstall
            </Button>
          </Card>
        ) : null}

        {setup === "leftovers" ? (
          <Card className="flex flex-col gap-3 p-6">
            <CardTitle>Remove the unfinished setup</CardTitle>
            <CardDescription>
              This computer has files and settings from a clinic setup that did not finish.
              Backups are kept. Keep your separately saved backup recovery file to restore them.
            </CardDescription>
            <Button
              variant="destructive"
              className="self-start"
              disabled={locked}
              onClick={() => void removeLeftovers()}
            >
              {working ? <Spinner /> : null}
              Remove everything
            </Button>
          </Card>
        ) : null}

        {busy ? (
          <div className="flex items-center gap-2.5 text-[13px] text-muted-foreground">
            <Spinner />
            {busyLabel || "Working"}…
          </div>
        ) : null}
        {error ? <div className="text-[12.5px] text-danger-ink">{error}</div> : null}

        <Button className="self-start" disabled={locked} onClick={() => void bridge.ExitUninstall()}>
          Keep CARE Desktop
        </Button>
      </ScreenBody>
    </Screen>
  );
}
