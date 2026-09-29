// The install is a long stream of build output; these patterns turn it into the
// steps the operator actually sees. First match wins, and progress only
// ever moves forward, so a late line from an earlier stage can't rewind the bar.
export type RunStep = { re: RegExp; pct: number; label: string };

export const RUN_STEPS: RunStep[] = [
  { re: /secret key/i, pct: 8, label: "Preparing the configuration" },
  { re: /Building CARE's images/i, pct: 15, label: "Building CARE" },
  { re: /secure gateway so this computer|Setting up this computer/i, pct: 25, label: "Setting up this computer" },
  { re: /backup encryption/i, pct: 35, label: "Securing the backups" },
  { re: /finish building/i, pct: 45, label: "Building CARE (the longest step)" },
  { re: /Starting CARE/i, pct: 90, label: "Starting the services" },
  { re: /database migrations/i, pct: 94, label: "Setting up the database" },
  { re: /become healthy|CARE is up/i, pct: 97, label: "Waiting for CARE to answer" },
];
