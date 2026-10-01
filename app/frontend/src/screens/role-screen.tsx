import { useState } from "react";

import { AppUpdateCard } from "@/components/app-update-card";
import { Screen, ScreenBody, ScreenHead } from "@/components/screen";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";

export function RoleScreen() {
  const { selectRole, busy: updating } = useCare();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const choose = async (role: "server" | "client") => {
    setBusy(true);
    setError("");
    try {
      await selectRole(role);
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Screen>
      <ScreenHead
        kicker="CARE Desktop"
        title="How will you use this computer?"
        subtitle="Choose how to use this computer. To change this later, uninstall its current setup first."
      />
      <ScreenBody className="flex flex-col gap-4">
        {error ? <Alert variant="danger" role="alert">{error}</Alert> : null}
        <div className="grid gap-4 md:grid-cols-2">
          <Card className="flex flex-col gap-4 p-6">
            <h2 className="text-lg font-bold text-ink">Set up a clinic</h2>
            <p className="flex-1 text-sm text-muted-foreground">
              Use this computer as the clinic server. Install and manage CARE here,
              and keep it running for other computers on the clinic network.
            </p>
            <Button variant="primary" disabled={busy || updating} onClick={() => void choose("server")}>
              Use as server
            </Button>
          </Card>
          <Card className="flex flex-col gap-4 p-6">
            <h2 className="text-lg font-bold text-ink">Connect to a clinic</h2>
            <p className="flex-1 text-sm text-muted-foreground">
              CARE is already installed on another computer. Enter its clinic address,
              approve its certificate with your computer administrator, and open CARE.
              No server installation is needed here.
            </p>
            <Button variant="primary" disabled={busy || updating} onClick={() => void choose("client")}>
              Use as client
            </Button>
          </Card>
        </div>
        <AppUpdateCard disabled={busy} />
      </ScreenBody>
    </Screen>
  );
}
