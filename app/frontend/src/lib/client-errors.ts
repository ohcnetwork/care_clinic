import { errorText } from "@/lib/format";

export type FriendlyError = {
  title: string;
  message: string;
  tips?: string[];
  action?: "setup" | "disconnect" | "repair";
};

type Rule = { match: RegExp; error: FriendlyError };

const sameNetworkTips = [
  "Make sure this computer is on the same Wi-Fi or network as the clinic's main computer.",
  "Check that the clinic's main computer is switched on and CARE Clinic is running on it.",
  "Check the address for spelling mistakes.",
];

const rules: Rule[] = [
  {
    match: /^this computer has an unfinished clinic setup/i,
    error: {
      title: "This computer has an unfinished clinic setup",
      message: "It was set up as a clinic computer before. Remove that in Setup first, then come back and connect.",
      action: "setup",
    },
  },
  {
    match: /^this computer is sending the clinic address to itself/i,
    error: {
      title: "This computer is pointing the clinic address at itself",
      message: "An old setting sends the clinic address to this computer instead of the clinic's server. Connecting removes it.",
      tips: ["Click Connect and fix, and approve the request when your computer asks."],
      action: "repair",
    },
  },
  {
    match: /^could not save the clinic connection/i,
    error: {
      title: "The connection couldn't be saved",
      message: "No certificate was installed. Try again. If it keeps happening, restart this computer.",
    },
  },
  {
    match: /^could not (install|remove) the clinic certificate[\s\S]*(cancel|-128)/i,
    error: {
      title: "Permission was not given",
      message:
        "Your computer asked for permission to trust the clinic, but it was not approved.",
      tips: [
        "Try again.",
        "When your computer asks, enter the password you use to log in to this computer, then click OK or Yes.",
        "If you don't know the password, ask the person who looks after this computer.",
      ],
    },
  },
  {
    match: /^(enter the clinic address|enter a clinic address|enter just the clinic address|use the clinic's local address)/i,
    error: {
      title: "That address doesn't look right",
      message: "Type only the clinic's name, for example care, with nothing else before or after it.",
      tips: ["You can find the address on the clinic's main computer, in CARE Clinic."],
    },
  },
  {
    match: /^could not reach the clinic/i,
    error: {
      title: "We couldn't find the clinic",
      message: "This computer could not reach the clinic at that address.",
      tips: sameNetworkTips,
    },
  },
  {
    match: /^(the clinic could not provide|the clinic did not provide|the clinic returned an oversized|this is not a CARE clinic|could not read the clinic certificate|could not download the clinic certificate)/i,
    error: {
      title: "This doesn't look like a CARE clinic",
      message: "Something answered at this address, but it isn't a CARE clinic we can connect to.",
      tips: [
        "Check the address with your clinic administrator.",
        "If the address is right, ask them to restart CARE on the clinic's main computer.",
      ],
    },
  },
  {
    match: /^the clinic certificate is not valid now/i,
    error: {
      title: "Check this computer's date and time",
      message: "The clinic's security check failed because this computer's clock may be wrong.",
      tips: [
        "Set this computer's date and time to the correct values, then try again.",
        "If the clock is right, ask your clinic administrator for help.",
      ],
    },
  },
  {
    match: /^could not verify the secure connection/i,
    error: {
      title: "We couldn't connect securely",
      message: "The clinic was found, but this computer could not confirm that the connection is safe.",
      tips: [
        ...sameNetworkTips.slice(0, 2),
        "If the clinic's main computer was set up again recently, disconnect this computer below and connect again.",
      ],
    },
  },
  {
    match: /^(could not install the clinic certificate|the clinic certificate was not installed)/i,
    error: {
      title: "Your computer didn't allow the connection",
      message: "CARE couldn't finish setting up trust with the clinic on this computer.",
      tips: [
        "Click Connect again and approve the request when your computer asks.",
        "If it keeps failing, ask the person who looks after this computer for help and share the log file.",
      ],
    },
  },
  {
    match: /^could not (check|remove) .*hosts file/i,
    error: {
      title: "Your computer needs a quick fix first",
      message:
        "This computer has an old setting that sends the clinic address to the wrong place. CARE needs your permission to remove it.",
      tips: [
        "Try again.",
        "When your computer asks, enter the password you use to log in to this computer, then click OK or Yes.",
        "If you don't know the password, ask the person who looks after this computer.",
      ],
    },
  },
  {
    match: /^remove this computer's current clinic access/i,
    error: {
      title: "This computer is set up for another clinic",
      message: "This computer is set up for a different clinic. Disconnect it first, then connect to the new one.",
      action: "disconnect",
    },
  },
  {
    match: /^(could not remove the clinic certificate|the clinic certificate is still installed)/i,
    error: {
      title: "Couldn't disconnect this computer",
      message: "Your computer didn't allow CARE to remove the clinic's security setting.",
      tips: ["Try again and approve the request when your computer asks."],
    },
  },
  {
    match: /^(something else is still running|CARE Clinic is closing)/i,
    error: {
      title: "Please wait a moment",
      message: "CARE Clinic is still finishing another task. Try again in a few seconds.",
    },
  },
];

export function friendlyClientError(e: unknown): FriendlyError {
  const text = errorText(e).trim();
  const rule = rules.find((r) => r.match.test(text));
  if (rule) return rule.error;
  return {
    title: "Something went wrong",
    message: "CARE couldn't connect to the clinic.",
    tips: ["Try again. If it keeps happening, ask your clinic administrator for help and share the log file."],
  };
}
