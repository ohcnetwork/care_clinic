// The setup form lives above its screen so that bouncing off a failed install
// never loses what the operator already typed.
export type SetupForm = {
  /** Without the ".local" suffix, which the field shows as a fixed adornment. */
  hostInput: string;
  adminPassword: string;
  adminConfirm: string;
  backupDir: string;
};

export const EMPTY_SETUP_FORM: SetupForm = {
  hostInput: "care",
  adminPassword: "",
  adminConfirm: "",
  backupDir: "",
};
