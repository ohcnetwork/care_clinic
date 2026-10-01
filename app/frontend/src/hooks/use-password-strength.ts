import { useEffect, useState } from "react";

import { bridge, logToHost } from "@/lib/bridge";

export const DEFAULT_PW_MESSAGE =
  "Use 8 to 20 characters with an uppercase letter, a lowercase letter and a number.";

export type PasswordStrength = { strong: boolean; message: string };

/**
 * Strength comes from the Go validator, so the rule lives in exactly one place.
 * Debounced, and late replies for a value the operator has moved past are
 * dropped rather than painted.
 */
export function usePasswordStrength(value: string): PasswordStrength {
  const [strength, setStrength] = useState<PasswordStrength & { value: string }>({
    value: "",
    strong: false,
    message: DEFAULT_PW_MESSAGE,
  });

  useEffect(() => {
    if (value === "") {
      setStrength({ value, strong: false, message: DEFAULT_PW_MESSAGE });
      return;
    }
    let live = true;
    const id = window.setTimeout(() => {
      void bridge.ValidatePassword(value).then(
        (reason) => {
          if (!live) return;
          setStrength({
            value,
            strong: reason === "",
            message: reason === "" ? "Strong password." : reason,
          });
        },
        () => {
          if (!live) return;
          logToHost("password validation failed");
          setStrength({ value, strong: false, message: "Couldn't check the password. Try typing it again." });
        },
      );
    }, 180);
    return () => {
      live = false;
      window.clearTimeout(id);
    };
  }, [value]);

  return strength.value === value ? strength : { strong: false, message: value ? "Checking password…" : DEFAULT_PW_MESSAGE };
}
