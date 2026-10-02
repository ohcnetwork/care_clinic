// Everyday clinic choices. Other local overrides belong in the support-only
// editor; protected wiring and unsupported online services remain hidden.
// Fallbacks describe CARE's behavior, not values to write over saved settings.
import type { Section } from "@/types";

export type Option = { value: string; label: string };

type Base = { key: string; file: Section; label: string; help?: string };

export type Setting = Base &
  (
    | { kind: "radio"; options: Option[]; fallback: string }
    | { kind: "select"; options: Option[]; fallback: string; none?: string }
    | { kind: "multi"; options: Option[]; fallback: string[] }
    | {
        kind: "int";
        min?: number;
        max?: number;
        unit?: string;
        fallback?: string;
        /** Shipped in the file; clearing it is not allowed. */
        required?: boolean;
      }
    | { kind: "text"; placeholder?: string }
  );

export type Group = { id: string; title: string; summary: string; settings: Setting[] };

const YES_NO = (yes = "Yes", no = "No"): Option[] => [
  { value: "true", label: yes },
  { value: "false", label: no },
];

export const VISIT_OPTIONS: Option[] = [
  { value: "amb", label: "Outpatient visit" },
  { value: "imp", label: "Inpatient admission" },
  { value: "emer", label: "Emergency" },
  { value: "obsenc", label: "Observation" },
  { value: "hh", label: "Home visit" },
  { value: "vr", label: "Remote consultation" },
];

export const GROUPS: Group[] = [
  {
    id: "branding",
    title: "Clinic details",
    summary: "The name and languages staff see in CARE",
    settings: [
      {
        key: "REACT_APP_TITLE",
        file: "frontend",
        kind: "text",
        label: "Clinic display name",
        help: "Shown on browser tabs. This does not change the clinic's registered name inside CARE.",
        placeholder: "CARE",
      },
      {
        key: "REACT_ALLOWED_LOCALES",
        file: "frontend",
        kind: "multi",
        label: "Languages for staff",
        help: "Staff can choose from these languages when using CARE.",
        options: [
          { value: "en", label: "English" },
          { value: "hi", label: "हिन्दी" },
          { value: "ta", label: "தமிழ்" },
          { value: "ml", label: "മലയാളം" },
          { value: "mr", label: "मराठी" },
          { value: "kn", label: "ಕನ್ನಡ" },
        ],
        fallback: ["en", "hi", "ta", "ml", "mr", "kn"],
      },
    ],
  },
  {
    id: "visits",
    title: "Patients and visits",
    summary: "Make patient registration and new visits quicker",
    settings: [
      {
        key: "REACT_DEFAULT_ENCOUNTER_TYPE",
        file: "frontend",
        kind: "select",
        label: "Usual visit type",
        help: "Fill in this choice when a new visit starts. Staff can still change it.",
        options: VISIT_OPTIONS,
        fallback: "",
        none: "No preference",
      },
      {
        key: "REACT_ENABLE_MINIMAL_PATIENT_REGISTRATION",
        file: "frontend",
        kind: "radio",
        label: "Use a shorter registration form",
        help: "Let staff register a patient with fewer details.",
        options: YES_NO(),
        fallback: "false",
      },
    ],
  },
  {
    id: "billing",
    title: "Billing",
    summary: "How your clinic records payments and prepares bills",
    settings: [
      {
        key: "REACT_DEFAULT_PAYMENT_METHOD",
        file: "frontend",
        kind: "select",
        label: "Usual payment method",
        help: "Fill in this choice for new payments. Staff can change it for each payment.",
        options: [
          { value: "cash", label: "Cash" },
          { value: "ccca", label: "Credit card" },
          { value: "debc", label: "Debit card" },
          { value: "chck", label: "Cheque" },
          { value: "ddpo", label: "Direct deposit" },
          { value: "cdac", label: "Credit account" },
          { value: "cchk", label: "Credit check" },
        ],
        fallback: "",
        none: "No preference",
      },
      {
        key: "REACT_DEFAULT_PAYMENT_TERMS",
        file: "frontend",
        kind: "text",
        label: "Payment instructions on bills",
        help: "A short message printed on invoices.",
        placeholder: "Please pay at the reception desk",
      },
      {
        key: "REACT_INVENTORY_DEFAULT_TAX_INCLUSIVE",
        file: "frontend",
        kind: "radio",
        label: "Prices already include tax",
        help: "Choose Yes when staff enter the final price, such as the MRP printed on a product.",
        options: YES_NO(),
        fallback: "false",
      },
      {
        key: "REACT_ENABLE_AUTO_INVOICE_AFTER_DISPENSE",
        file: "frontend",
        kind: "radio",
        label: "Open the bill after dispensing medicines",
        help: "Show the invoice screen when dispensing is finished.",
        options: YES_NO(),
        fallback: "false",
      },
    ],
  },
  {
    id: "backups",
    title: "Backups",
    summary: "How long to keep safe copies of your clinic's records",
    settings: [
      {
        key: "DB_BACKUP_RETENTION_PERIOD",
        file: "backend",
        kind: "int",
        label: "Keep backups for",
        help: "Older backups, including ones you started yourself, are deleted automatically. Enter 0 to keep them all.",
        unit: "days",
        min: 0,
        max: 3650,
        fallback: "0",
        required: true,
      },
    ],
  },
  {
    id: "signin",
    title: "Staff access",
    summary: "Protect records when a staff member leaves their computer",
    settings: [
      {
        key: "JWT_REFRESH_TOKEN_LIFETIME",
        file: "backend",
        kind: "int",
        label: "Sign out inactive staff after",
        help: "Staff will need to sign in again after this long without activity. Shorter times are safer on shared computers.",
        unit: "minutes",
        min: 5,
        max: 43200,
        fallback: "30",
      },
    ],
  },
];

export const SETTINGS: Setting[] = GROUPS.flatMap((g) => g.settings);
export const SETTING_BY_KEY = new Map(SETTINGS.map((s) => [s.key, s]));

// Internal wiring and unsupported service settings are preserved, never
// editable through the friendly controls or the custom settings form.
const HIDDEN_KEYS = new Set([
  "DJANGO_SETTINGS_MODULE", "DATABASE_URL", "REDIS_URL", "CELERY_BROKER_URL",
  "DJANGO_SECRET_KEY", "DJANGO_DEBUG", "DJANGO_ALLOWED_HOSTS", "DJANGO_ADMIN_URL",
  "SENTRY_DSN", "CSRF_TRUSTED_ORIGINS", "FILE_UPLOAD_BUCKET", "FACILITY_S3_BUCKET",
  "JWT_ACCESS_TOKEN_LIFETIME", "ADDITIONAL_PLUGS", "PYTHONPATH",
  "REACT_CARE_API_URL", "REACT_RECAPTCHA_SITE_KEY", "REACT_CARE_URL_MAP",
  "REACT_SBOM_BASE_URL", "REACT_GITHUB_URL", "REACT_OHCN_URL",
  "REACT_JWT_TOKEN_REFRESH_INTERVAL", "REACT_ACCOUNTING_PRECISION",
  "REACT_MAX_DATAPOINTS_PER_UPSERT", "REACT_APP_UPDATE_CHECK_INTERVAL",
  "REACT_CUSTOM_REMOTE_I18N_URL", "REACT_ENABLED_APPS", "CARE_CDN_URL",
  "USE_SMS", "ENABLE_OTP_LOGIN", "ENABLE_MFA", "DEFAULT_FROM_EMAIL", "SERVER_EMAIL",
  "REACT_DISABLE_PATIENT_LOGIN", "REACT_APP_RESEND_OTP_TIMEOUT", "REACT_ENABLE_MFA",
]);
const HIDDEN_PREFIXES = [
  "POSTGRES_", "MINIO_", "BUCKET_", "DJANGO_SECURE_",
  "REACT_PUBLIC", "REACT_SENTRY", "REACT_APP_META", "REACT_PAGINATION_", "REACT_DECIMAL_",
  "EMAIL_", "SMTP_", "SMS_", "SNS_", "OTP_", "MFA_", "TOTP_", "RECAPTCHA_",
  "REACT_EMAIL_", "REACT_SMS_", "REACT_OTP_", "REACT_MFA_", "REACT_TOTP_", "REACT_RECAPTCHA_",
];

export function isHiddenKey(key: string): boolean {
  return HIDDEN_KEYS.has(key) || HIDDEN_PREFIXES.some((p) => key.startsWith(p));
}

/** Keys CARE Desktop owns, and where their configuration belongs. */
export const MANAGED_NOTES: Record<string, string> = {
  DJANGO_SECRET_KEY: "Generated and protected by CARE Desktop.",
  CSRF_TRUSTED_ORIGINS: "The clinic address is managed by CARE Desktop.",
  BUCKET_EXTERNAL_ENDPOINT: "The clinic address is managed by CARE Desktop.",
  REACT_CARE_API_URL: "The clinic address is managed by CARE Desktop.",
  ADDITIONAL_PLUGS: "Use the Plugins tab instead.",
};

/** Every frontend key is REACT_-prefixed; anything else belongs to the server. */
export function fileForKey(key: string): Section {
  return key.startsWith("REACT_") ? "frontend" : "backend";
}
