import { FolderOpen, Lock, ScrollText } from "lucide-react";
import { useEffect, useState } from "react";

import { InputBox } from "@/components/input-box";
import { SectionTitle } from "@/components/section-header";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import { EnvEditor } from "./env-editor";
import { PluginTable } from "./plugin-table";
import { UpdatePanel } from "./update-panel";

export function AdvancedTab() {
  const [adminPassword, setAdminPassword] = useState<string | null>(null);

  if (adminPassword === null) return <AdminGate onUnlock={setAdminPassword} />;

  return (
    <div className="flex flex-col gap-3">
      <RebuildCard adminPassword={adminPassword} />
      <LogRow />

      <Accordion type="multiple">
        <AccordionItem value="updates">
          <AccordionTrigger>
            <SectionTitle
              title="Updates"
              summary="Which version of CARE this clinic runs, and this app's own version"
            />
          </AccordionTrigger>
          <AccordionContent>
            <UpdatePanel />
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="config">
          <AccordionTrigger>
            <SectionTitle
              title="Clinic settings"
              summary="Backups, sign-in, SMS, email, and what staff see in the app"
            />
          </AccordionTrigger>
          <AccordionContent>
            <EnvEditor adminPassword={adminPassword} />
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="plugins">
          <AccordionTrigger>
            <SectionTitle title="Plugins" summary="Extra features for CARE" />
          </AccordionTrigger>
          <AccordionContent>
            <PluginTable adminPassword={adminPassword} />
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="danger" className="border-danger-line">
          <AccordionTrigger>
            <SectionTitle
              title={<span className="text-danger-ink">Uninstall CARE Desktop</span>}
              summary="Removes CARE and all patient data from this computer"
            />
          </AccordionTrigger>
          <AccordionContent>
            <UninstallPanel adminPassword={adminPassword} />
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

function RebuildCard({ adminPassword }: { adminPassword: string }) {
  const { busy, runAction } = useCare();
  return (
    <Card className="flex items-center gap-3.5 px-[18px] py-4">
      <div className="min-w-0 flex-1">
        <CardTitle>Rebuild everything</CardTitle>
        <CardDescription>
          Rebuilds CARE and restarts every service with this app's bundled files and current
          settings. Patient data is kept; CARE is unavailable for a few minutes.
        </CardDescription>
      </div>
      <Button disabled={busy} onClick={() => void runAction("rebuild-all", adminPassword)}>
        Rebuild
      </Button>
    </Card>
  );
}

export function AdminGate({ onUnlock }: { onUnlock: (password: string) => void }) {
  const [password, setPassword] = useState("");
  const [reveal, setReveal] = useState(false);
  const [error, setError] = useState("");
  const [checking, setChecking] = useState(false);

  const unlock = async () => {
    if (password === "") {
      setError("Enter the admin password.");
      return;
    }
    setChecking(true);
    try {
      if (await bridge.VerifyAdminPassword(password)) {
        onUnlock(password);
        return;
      }
      setError("That password does not match the admin password.");
    } catch {
      setError("Couldn't check the password.");
    } finally {
      setChecking(false);
    }
  };

  return (
    <div className="mx-auto mt-[34px] max-w-[420px] rounded-2xl border border-line bg-card p-[26px] text-center shadow-card">
      <span className="mx-auto flex size-10 items-center justify-center rounded-full bg-hair text-muted-foreground">
        <Lock className="size-[18px]" strokeWidth={2} />
      </span>
      <div className="mt-3.5 text-[17px] font-bold text-ink">Admin password</div>
      <div className="mt-[5px] text-[13px] text-muted-foreground">
        These options can rebuild or remove CARE.
      </div>
      <InputBox tone={error ? "bad" : "neutral"} className="mt-4">
        <Input
          type={reveal ? "text" : "password"}
          placeholder="Password"
          autoComplete="off"
          className="h-full flex-1 rounded-none border-none bg-transparent px-0 focus-visible:border-none"
          value={password}
          onChange={(e) => {
            setPassword(e.target.value);
            setError("");
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") void unlock();
          }}
        />
        <button
          type="button"
          onClick={() => setReveal((v) => !v)}
          className="cursor-pointer p-1 text-xs font-semibold text-muted-foreground hover:text-brand-ink"
        >
          {reveal ? "Hide" : "Show"}
        </button>
      </InputBox>
      {error ? (
        <div className="mt-2 text-left text-[12.5px] text-danger-ink">{error}</div>
      ) : null}
      <Button
        variant="primary"
        size="block"
        className="mt-3.5"
        disabled={checking}
        onClick={() => void unlock()}
      >
        Unlock
      </Button>
    </div>
  );
}

export function UninstallPanel({ adminPassword }: { adminPassword: string }) {
  const { busy, uninstall } = useCare();
  const [removeBackups, setRemoveBackups] = useState(false);
  const [removeImages, setRemoveImages] = useState(false);
  const [removeRancher, setRemoveRancher] = useState(false);
  const [showRancher, setShowRancher] = useState(false);
  const [removeApp, setRemoveApp] = useState(false);
  const [canRemoveApp, setCanRemoveApp] = useState(false);

  useEffect(() => {
    void bridge.RancherDesktopInstalled().then(setShowRancher, () => setShowRancher(false));
    void bridge.CanRemoveApp().then(setCanRemoveApp, () => setCanRemoveApp(false));
  }, []);
  const [confirming, setConfirming] = useState(false);

  return (
    <>
      <label className="flex cursor-pointer items-center gap-2.5 text-[13px] text-ink2">
        <Checkbox
          checked={removeBackups}
          onCheckedChange={(v) => setRemoveBackups(v === true)}
        />
        <span>Also delete backups. Nothing can be recovered afterwards.</span>
      </label>
      <label className="flex cursor-pointer items-center gap-2.5 text-[13px] text-ink2">
        <Checkbox checked={removeImages} onCheckedChange={(v) => setRemoveImages(v === true)} />
        <span>
          Also remove downloaded Docker images and clear Docker's build cache.
          <span className="text-muted-foreground">
            {" "}
            The build cache is shared, so this frees space other projects on this
            computer are using too.
          </span>
        </span>
      </label>
      {showRancher ? (
        <label className="flex cursor-pointer items-center gap-2.5 text-[13px] text-ink2">
          <Checkbox checked={removeRancher} onCheckedChange={(v) => setRemoveRancher(v === true)} />
          <span>
            Also remove Rancher Desktop and its settings.
            <span className="text-muted-foreground">
              {" "}
              Rancher Desktop runs Docker for CARE. Leave this unticked if other apps on this
              computer use Docker.
            </span>
          </span>
        </label>
      ) : null}
      {canRemoveApp ? (
        <label className="flex cursor-pointer items-center gap-2.5 text-[13px] text-ink2">
          <Checkbox checked={removeApp} onCheckedChange={(v) => setRemoveApp(v === true)} />
          <span>Also remove the CARE Desktop app from this computer.</span>
        </label>
      ) : null}

      {confirming ? (
        <div className="flex items-center gap-3 rounded-lg border border-danger-bg bg-danger-tint px-4 py-[13px] text-[12.5px] text-danger-ink">
          <span className="flex-1">
            Delete CARE and all patient data on this computer?
          </span>
          <Button onClick={() => setConfirming(false)}>Cancel</Button>
          <Button
            variant="destructive"
            onClick={() => {
              setConfirming(false);
              void uninstall(removeImages, removeBackups, removeRancher, adminPassword, removeApp);
            }}
          >
            Yes, delete
          </Button>
        </div>
      ) : (
        <Button
          variant="destructive"
          className="self-start"
          disabled={busy}
          onClick={() => setConfirming(true)}
        >
          Uninstall everything
        </Button>
      )}
    </>
  );
}

/**
 * Where the diagnostic log is written. Read-only on purpose: the path is fixed to
 * the platform's convention so that someone helping remotely can name the folder
 * without first asking where this install put it.
 */
function LogRow() {
  const { log } = useCare();
  const [path, setPath] = useState("");

  useEffect(() => {
    void bridge.LogPath().then(setPath, () => setPath(""));
  }, []);

  return (
    <div className="flex items-center gap-3 rounded-xl border border-line bg-card px-4 py-3.5 shadow-card">
      <span className="flex size-[30px] flex-none items-center justify-center rounded-sm bg-brand-bg text-brand-ink">
        <ScrollText className="size-4" strokeWidth={2} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-xs font-semibold tracking-[0.04em] text-muted-foreground uppercase">
          Diagnostic log
        </div>
        <div className="truncate font-mono text-[13.5px] font-semibold text-ink">
          {path || "not being written this run"}
        </div>
      </div>
      <Button
        disabled={!path}
        onClick={() => void bridge.OpenLogFolder().catch((e) => log(`log folder: ${errorText(e)}`))}
      >
        <FolderOpen className="size-4" strokeWidth={2} />
        Open
      </Button>
    </div>
  );
}
