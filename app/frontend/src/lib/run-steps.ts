// These log milestones are ordered, not measured progress. The legacy pct field
// is only a monotonic weight for the store; never display it as a percentage.
// Anchor to native messages so an error mentioning a stage can't advance it.
export type RunStep = { re: RegExp; pct: number; label: string };

export const RUN_STEPS: RunStep[] = [
  { re: /^Backup encryption ready;/i, pct: 5, label: "Protecting your backups" },
  { re: /^Generated a random DJANGO_SECRET_KEY in backend\.env/i, pct: 8, label: "Preparing settings" },
  { re: /^Building CARE's images/i, pct: 15, label: "Starting CARE builds" },
  { re: /^(?:Starting the secure gateway so this computer|Setting up this computer to open)/i, pct: 25, label: "Setting up this computer" },
  { re: /^Waiting for the backend and app images to finish building/i, pct: 45, label: "Building CARE — the longest step" },
  { re: /^Starting CARE\.\.\./i, pct: 90, label: "Starting the clinic" },
  { re: /^Applying database migrations/i, pct: 94, label: "Preparing the database" },
  { re: /^(?:Waiting for CARE to become healthy|CARE is up ->)/i, pct: 97, label: "Checking the clinic" },
];
