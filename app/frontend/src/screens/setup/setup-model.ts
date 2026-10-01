import type { SetupPage, SetupRecoveryStatus } from "@/types";

export type RequirementPage = "space" | "windows" | "software" | "cleanup" | "network";
export type StepState = "waiting" | "checking" | "ready" | "blocked" | "failed";
export const REQUIREMENT_PAGES: RequirementPage[] = ["space", "windows", "software", "cleanup", "network"];
export function isRequirement(page: SetupPage): page is RequirementPage {
  return page === "space" || page === "windows" || page === "software" || page === "cleanup" || page === "network";
}

export const SETUP_LABELS: Record<SetupPage, string> = {
  space: "Free space", windows: "Windows setup", software: "Required software",
  cleanup: "Cleanup stale files", network: "Network profile", address: "Clinic address",
  backup: "Backups", admin: "Admin password", review: "Review", install: "Install",
};

export const EMPTY_RECOVERY: SetupRecoveryStatus = {
  backup_saved: false, backup_verified: false, codes_saved: false,
  backup_path: "", codes_path: "", backup_problem: "", codes_problem: "",
};

export function backupProblem(raw: string): string {
  if (!raw) return "";
  if (/read.only/i.test(raw)) return "That location is read-only, so backups can't be written to it. Choose a different location.";
  if (/not a folder|file, not a folder/i.test(raw)) return "That's a file, not a folder. Choose a different location.";
  if (/isn't there|not available|no such file/i.test(raw)) return "The backup location isn't available any more. Reconnect it and check again, or choose a different location.";
  if (/not.*allowed|isn['’]t allowed|permission|access denied/i.test(raw)) return "This computer isn't allowed to write there. Ask whoever looks after this computer for access, or choose a different location.";
  if (/another CARE|another clinic|different clinic|foreign/i.test(raw)) return "That folder already holds backups from a different clinic. Leave those backups intact and choose a different location.";
  if (/free space|not enough|needs .*GB|too small/i.test(raw)) return "There isn't enough room for the first backup. Choose a different location with more room.";
  if (/recovery.*separate|recovery file/i.test(raw)) return "Keep the backup recovery file separate from the backup folder. Choose a different location.";
  if (/inside|outside|overlap|CARE|absolute/i.test(raw)) return "Backups can't be kept in that location. Keep them outside CARE's own folders and choose a different location.";
  return "That location couldn't be checked or written to. Check it again, or choose a different location.";
}

export function recoveryProblem(raw: string): string {
  if (/outside CARE|settings, installation|overlap/i.test(raw)) return "That folder belongs to CARE. Keep recovery materials outside CARE's settings, installation, logs and backup folder. Choose somewhere else.";
  if (/file exists|new filename/i.test(raw)) return "There's already a file with that name, or it couldn't be saved there. CARE never overwrites a file. Choose a new filename or location.";
  if (/match|different|decrypt/i.test(raw)) return "That file doesn't match this clinic. It may be from an earlier setup or a different clinic. Select the file you just saved.";
  if (/locked|already installed/i.test(raw)) return "Recovery setup is locked because installation has already started. Keep the recovery material you saved and return to the existing installation.";
  if (/read|open|invalid|private key|PEM/i.test(raw)) return "That file couldn't be opened. It may be damaged or incomplete. Select the correct file, or save a new recovery file before installing.";
  return "The recovery file couldn't be saved or checked. Try again. If it keeps happening, share the log file with your support contact.";
}
