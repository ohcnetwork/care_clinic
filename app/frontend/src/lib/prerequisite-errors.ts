import { errorText } from "@/lib/format";

export type PrerequisiteProblem = { title?: string; message: string };

export function downloadProblem(cause: unknown, preview = false): PrerequisiteProblem | null {
  const detail = errorText(cause);
  if (/download connection interrupted:|no progress|stopped making progress|download.*timed out/i.test(detail)) {
    return preview ? {
      title: "Couldn't reach the download server",
      message: "Check the internet connection, then try again. Nothing has been downloaded yet.",
    } : {
      title: "The download was interrupted",
      message: "Check the internet connection, then try again. The incomplete download wasn't installed. Trying again downloads the file from the beginning.",
    };
  }
  if (!preview && /sha.?256|checksum|hash mismatch/i.test(detail)) {
    return {
      title: "The downloaded file couldn't be verified",
      message: "The downloaded file isn't the one this version of CARE expects. It wasn't installed. Try again; if it happens twice, share the log file.",
    };
  }
  return null;
}
