import { RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Switch } from "@/components/ui/switch";
import { useCare } from "@/state/care-store";
import type { UpdateChecks } from "@/types";
import { AdvancedError, advancedProblem, useAdvancedLock, type AdvancedProblem } from "./advanced-ui";

const ROWS: { key: keyof UpdateChecks; label: string; help: string }[] = [
  {
    key: "app",
    label: "Check for CARE Clinic app updates",
    help: "When off, this app never contacts GitHub to look for a newer CARE Clinic version.",
  },
  {
    key: "care",
    label: "Check for CARE software updates",
    help: "When off, CARE stays on its current version: nothing is checked, prepared or installed in the background.",
  },
];

export function UpdateCheckSettings({ adminPassword }: { adminPassword: string }) {
  const { busy, updateChecks, reloadUpdateChecks, saveUpdateChecks } = useCare();
  const lock = useAdvancedLock();
  const [saving, setSaving] = useState<keyof UpdateChecks | null>(null);
  const [problem, setProblem] = useState<AdvancedProblem | null>(null);
  const pending = useRef(false);
  useEffect(() => { void reloadUpdateChecks(); }, [reloadUpdateChecks]);
  const blocked = busy || lock.disabled || saving !== null || updateChecks === null;
  const change = async (key: keyof UpdateChecks, on: boolean) => {
    if (!updateChecks || pending.current || busy || lock.isLocked()) return;
    pending.current = true;
    setSaving(key);
    setProblem(null);
    try {
      await saveUpdateChecks({ ...updateChecks, [key]: on }, adminPassword);
    } catch (cause) {
      setProblem(advancedProblem(cause, "The update setting couldn't be saved", "Nothing was changed. Try again when CARE Clinic is ready."));
    } finally {
      pending.current = false;
      setSaving(null);
    }
  };
  return <section className="advanced-card advanced-card-pad" aria-labelledby="advanced-update-checks-title">
    <div className="advanced-card-row">
      <span className="advanced-icon"><RefreshCw aria-hidden="true" /></span>
      <div className="advanced-grow">
        <h2 className="advanced-card-title" id="advanced-update-checks-title">Update checks</h2>
        <p className="advanced-card-description">Choose which updates this computer looks for. Changes apply straight away.</p>
      </div>
    </div>
    {ROWS.map((row) => {
      const on = updateChecks?.[row.key] !== false;
      return <div className="advanced-setting" key={row.key}>
        <div>
          <div className="advanced-setting-label">{row.label}</div>
          <p className="advanced-setting-help">{row.help}</p>
        </div>
        <div className="advanced-setting-control">
          <div className="advanced-switch-control">
            <Switch checked={on} disabled={blocked} aria-label={row.label}
              onCheckedChange={(checked) => void change(row.key, checked)} />
            <span>{saving === row.key ? "Saving…" : on ? "On" : "Off"}</span>
          </div>
        </div>
      </div>;
    })}
    <AdvancedError problem={problem} />
  </section>;
}
