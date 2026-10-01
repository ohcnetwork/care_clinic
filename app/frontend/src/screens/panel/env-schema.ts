// The settings a clinic may actually want to change, described so the editor
// can draw the right control for each: a yes/no as radio chips, a number as a
// number box, a fixed choice as a select, and nothing else. Every other key in
// backend.env / frontend.env is either wiring CARE Desktop manages (HIDDEN) or
// something for the "Other settings" list at the bottom.
//
// `fallback` is what CARE does when the key is absent from the file, so the
// control can show the effective value rather than an empty state. Sources:
// care/config/settings/*.py and care_fe/care.config.ts.
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
    | {
        kind: "text" | "secret";
        placeholder?: string;
        /** Values the shipped file uses to mean "not set". */
        blank?: string[];
      }
    | { kind: "logo" }
  );

export type Group = { id: string; title: string; summary: string; settings: Setting[] };

const YES_NO = (yes = "Yes", no = "No"): Option[] => [
  { value: "true", label: yes },
  { value: "false", label: no },
];

const PLACEHOLDER = ["123"];

export const GROUPS: Group[] = [
  {
    id: "backups",
    title: "Backups",
    summary: "Automatic backups every 24 hours, and how long their files are kept",
    settings: [
      {
        key: "DB_BACKUP_RETENTION_PERIOD",
        file: "backend",
        kind: "int",
        label: "Keep backups for",
        help: "Older files, including manual backups, are deleted after each automatic backup. 0 keeps every backup forever.",
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
    title: "Staff sign-in and security",
    summary: "Password protection, idle sign-out and suspicious web requests",
    settings: [
      {
        key: "DISABLE_RATELIMIT",
        file: "backend",
        kind: "radio",
        label: "Slow down repeated wrong passwords",
        help: "A device gets 5 tries per 10 minutes. Recommended once the clinic is live.",
        options: [
          { value: "False", label: "On" },
          { value: "True", label: "Off" },
        ],
        fallback: "False",
      },
      {
        key: "JWT_REFRESH_TOKEN_LIFETIME",
        file: "backend",
        kind: "int",
        label: "Sign staff out after being idle for",
        help: "Raise it if people complain about being signed out; lower it for shared computers.",
        unit: "minutes",
        min: 5,
        max: 43200,
        fallback: "30",
      },
      {
        key: "CORAZA_MODE",
        file: "backend",
        kind: "radio",
        label: "Protect against suspicious web requests",
        help: "Watch only notes them in the log. Block can stop legitimate use if a rule misfires.",
        options: [
          { value: "Off", label: "Off" },
          { value: "DetectionOnly", label: "Watch only" },
          { value: "On", label: "Block" },
        ],
        fallback: "Off",
      },
    ],
  },
  {
    id: "patients",
    title: "Patient sign-in",
    summary: "Whether patients can sign in themselves, and the SMS codes they need",
    settings: [
      {
        key: "REACT_DISABLE_PATIENT_LOGIN",
        file: "frontend",
        kind: "radio",
        label: "Let patients sign in to CARE",
        help: "No leaves staff sign-in only. Yes needs the SMS codes below to be set up.",
        options: [
          { value: "false", label: "Yes" },
          { value: "true", label: "No" },
        ],
        fallback: "false",
      },
      {
        key: "USE_SMS",
        file: "backend",
        kind: "radio",
        label: "Send sign-in codes by SMS",
        help: "Off, the code is only written to the log. Sending real SMS needs an Amazon SNS account and internet access.",
        options: [
          { value: "True", label: "On" },
          { value: "False", label: "Off" },
        ],
        fallback: "True",
      },
      {
        key: "SMS_BACKEND",
        file: "backend",
        kind: "radio",
        label: "SMS service",
        help: "None only writes the code to the log, which is fine while patients do not sign in.",
        options: [
          { value: "care.utils.sms.backend.sns.SnsBackend", label: "Amazon SNS" },
          { value: "care.utils.sms.backend.console.ConsoleBackend", label: "None" },
        ],
        fallback: "care.utils.sms.backend.sns.SnsBackend",
      },
      {
        key: "SNS_ACCESS_KEY",
        file: "backend",
        kind: "secret",
        label: "Amazon SNS access key",
        placeholder: "From your Amazon account",
        blank: PLACEHOLDER,
      },
      {
        key: "SNS_SECRET_KEY",
        file: "backend",
        kind: "secret",
        label: "Amazon SNS secret key",
        placeholder: "From your Amazon account",
        blank: PLACEHOLDER,
      },
      {
        key: "SNS_REGION",
        file: "backend",
        kind: "text",
        label: "Amazon region",
        placeholder: "ap-south-1",
      },
      {
        key: "OTP_VALIDITY_MINUTES",
        file: "backend",
        kind: "int",
        label: "A code stays valid for",
        unit: "minutes",
        min: 1,
        max: 1440,
        fallback: "10",
      },
      {
        key: "OTP_MAX_FAILURES",
        file: "backend",
        kind: "int",
        label: "Wrong codes allowed before a phone number is locked",
        min: 1,
        max: 100,
        fallback: "5",
      },
      {
        key: "OTP_LOCKOUT_MINUTES",
        file: "backend",
        kind: "int",
        label: "That lock lasts",
        unit: "minutes",
        min: 1,
        max: 10080,
        fallback: "60",
      },
      {
        key: "REACT_APP_RESEND_OTP_TIMEOUT",
        file: "frontend",
        kind: "int",
        label: "Wait before “resend code” is offered",
        unit: "seconds",
        min: 5,
        max: 600,
        fallback: "30",
      },
    ],
  },
  {
    id: "email",
    title: "Outgoing email",
    summary: "Only needed if CARE should send emails, such as password resets",
    settings: [
      {
        key: "EMAIL_HOST",
        file: "backend",
        kind: "text",
        label: "Mail server",
        placeholder: "smtp.gmail.com",
        blank: PLACEHOLDER,
      },
      {
        key: "EMAIL_PORT",
        file: "backend",
        kind: "int",
        label: "Mail server port",
        help: "Usually 587.",
        min: 1,
        max: 65535,
        fallback: "587",
      },
      {
        key: "EMAIL_USER",
        file: "backend",
        kind: "text",
        label: "Mail sign-in name",
        placeholder: "clinic@example.com",
        blank: PLACEHOLDER,
      },
      {
        key: "EMAIL_PASSWORD",
        file: "backend",
        kind: "secret",
        label: "Mail password",
        blank: PLACEHOLDER,
      },
      {
        key: "EMAIL_FROM",
        file: "backend",
        kind: "text",
        label: "Sender shown to recipients",
        placeholder: "Sunrise Clinic <noreply@sunrise.example>",
      },
    ],
  },
  {
    id: "branding",
    title: "Name and branding",
    summary: "What staff see in the browser tab and on the sign-in page",
    settings: [
      {
        key: "REACT_APP_TITLE",
        file: "frontend",
        kind: "text",
        label: "Name in the browser tab",
        placeholder: "CARE",
      },
      {
        key: "REACT_MAIN_LOGO",
        file: "frontend",
        kind: "logo",
        label: "Logo in the top bar",
        help: "Addresses must be reachable from every device in the clinic; an internet address will not load on an offline network.",
      },
      {
        key: "REACT_CUSTOM_DESCRIPTION",
        file: "frontend",
        kind: "text",
        label: "Line of text on the sign-in page",
        placeholder: "Welcome to Sunrise Clinic",
      },
    ],
  },
  {
    id: "region",
    title: "Language and region",
    summary: "Languages staff can switch between, and phone-number defaults",
    settings: [
      {
        key: "REACT_ALLOWED_LOCALES",
        file: "frontend",
        kind: "multi",
        label: "Languages",
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
      {
        key: "REACT_DEFAULT_COUNTRY",
        file: "frontend",
        kind: "text",
        label: "Country code for phone numbers",
        help: "Two letters, for example IN.",
        placeholder: "IN",
      },
      {
        key: "REACT_DEFAULT_COUNTRY_NAME",
        file: "frontend",
        kind: "text",
        label: "Country name",
        placeholder: "India",
      },
    ],
  },
  {
    id: "visits",
    title: "Visits and appointments",
    summary: "Which kinds of visit the clinic records, and what is pre-selected",
    settings: [
      {
        key: "REACT_ALLOWED_ENCOUNTER_CLASSES",
        file: "frontend",
        kind: "multi",
        label: "Kinds of visit",
        help: "Removing kinds you never use keeps forms shorter.",
        options: [
          { value: "amb", label: "Outpatient" },
          { value: "imp", label: "Inpatient" },
          { value: "emer", label: "Emergency" },
          { value: "obsenc", label: "Observation" },
          { value: "hh", label: "Home health" },
          { value: "vr", label: "Virtual" },
        ],
        fallback: ["imp", "amb", "obsenc", "emer", "vr", "hh"],
      },
      {
        key: "REACT_DEFAULT_ENCOUNTER_TYPE",
        file: "frontend",
        kind: "select",
        label: "Kind of visit pre-selected",
        help: "When only one kind is allowed, it is pre-selected anyway.",
        options: [
          { value: "amb", label: "Outpatient" },
          { value: "imp", label: "Inpatient" },
          { value: "emer", label: "Emergency" },
          { value: "obsenc", label: "Observation" },
          { value: "hh", label: "Home health" },
          { value: "vr", label: "Virtual" },
        ],
        fallback: "",
        none: "None, staff choose",
      },
      {
        key: "REACT_DEFAULT_DISCHARGE_DISPOSITION",
        file: "frontend",
        kind: "select",
        label: "Discharge outcome pre-selected",
        options: [
          { value: "home", label: "Home" },
          { value: "other_hcf", label: "Other health care facility" },
          { value: "aadvice", label: "Left against advice" },
          { value: "exp", label: "Expired" },
          { value: "alt_home", label: "Alternate home" },
          { value: "hosp", label: "Hospice" },
          { value: "long", label: "Long term care" },
          { value: "psy", label: "Psychiatric hospital" },
          { value: "rehab", label: "Rehabilitation" },
          { value: "snf", label: "Skilled nursing facility" },
          { value: "oth", label: "Other" },
        ],
        fallback: "",
        none: "None, staff choose",
      },
      {
        key: "REACT_ENCOUNTER_DEFAULT_DATE_FILTER",
        file: "frontend",
        kind: "int",
        label: "Visits list shows the last",
        help: "0 shows today only.",
        unit: "days",
        min: 0,
        max: 3650,
        fallback: "0",
      },
      {
        key: "REACT_APPOINTMENTS_DEFAULT_DATE_FILTER",
        file: "frontend",
        kind: "int",
        label: "Appointments list shows",
        help: "0 is today. A positive number looks that many days ahead, a negative one that many days back.",
        unit: "days",
        min: -3650,
        max: 3650,
        fallback: "0",
      },
      {
        key: "REACT_OPEN_SCHEDULE_AFTER_PATIENT_REGISTRATION",
        file: "frontend",
        kind: "radio",
        label: "Open the appointment scheduler right after a new patient is registered",
        options: YES_NO(),
        fallback: "false",
      },
      {
        key: "REACT_AUTO_REFRESH_BY_DEFAULT",
        file: "frontend",
        kind: "radio",
        label: "Refresh the appointment queue on its own",
        options: YES_NO(),
        fallback: "false",
      },
      {
        key: "REACT_AUTO_REFRESH_INTERVAL",
        file: "frontend",
        kind: "int",
        label: "Refresh every",
        unit: "seconds",
        min: 3,
        max: 3600,
        fallback: "10",
      },
    ],
  },
  {
    id: "registration",
    title: "Patient registration",
    summary: "How much the front desk must fill in",
    settings: [
      {
        key: "REACT_ENABLE_MINIMAL_PATIENT_REGISTRATION",
        file: "frontend",
        kind: "radio",
        label: "Quick registration",
        help: "Yes makes some registration fields optional, for fast front-desk entry.",
        options: YES_NO(),
        fallback: "false",
      },
      {
        key: "REACT_PATIENT_REG_MIN_GEO_ORG_LEVELS_REQUIRED",
        file: "frontend",
        kind: "int",
        label: "Address levels that must be filled in",
        help: "State, then district, and so on. Leave empty to require all of them.",
        min: 1,
        max: 10,
        fallback: "all",
      },
      {
        key: "REACT_PATIENT_GLOBAL_EDIT_ACCESS_ENABLED",
        file: "frontend",
        kind: "radio",
        label: "Any staff member can edit any patient's details",
        help: "Yes bypasses the usual role checks on editing.",
        options: YES_NO(),
        fallback: "false",
      },
      {
        key: "REACT_ENABLE_TOKEN_GENERATION_IN_PATIENT_HOME",
        file: "frontend",
        kind: "radio",
        label: "Show “Generate token” on the patient page",
        options: YES_NO(),
        fallback: "false",
      },
    ],
  },
  {
    id: "billing",
    title: "Billing and pharmacy",
    summary: "Invoices, payments and dispensing",
    settings: [
      {
        key: "REACT_DEFAULT_PAYMENT_TERMS",
        file: "frontend",
        kind: "text",
        label: "Payment terms printed on invoices",
        placeholder: "Payable at the counter on the day of the visit",
      },
      {
        key: "REACT_DEFAULT_PAYMENT_METHOD",
        file: "frontend",
        kind: "select",
        label: "Payment method pre-selected",
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
        none: "None, staff choose",
      },
      {
        key: "REACT_PAYMENT_LOCATION_REQUIRED",
        file: "frontend",
        kind: "radio",
        label: "A location must be chosen when recording a payment",
        options: YES_NO(),
        fallback: "true",
      },
      {
        key: "REACT_ENABLE_AUTO_INVOICE_AFTER_DISPENSE",
        file: "frontend",
        kind: "radio",
        label: "Open an invoice automatically after medicines are dispensed",
        options: YES_NO(),
        fallback: "false",
      },
      {
        key: "REACT_INVENTORY_DEFAULT_TAX_INCLUSIVE",
        file: "frontend",
        kind: "radio",
        label: "Entered prices include tax (MRP)",
        help: "Yes works the base price out from the price entered.",
        options: YES_NO(),
        fallback: "false",
      },
      {
        key: "REACT_INVENTORY_EXPIRY_MONTH_OFFSET",
        file: "frontend",
        kind: "select",
        label: "Stop dispensing stock that expires",
        options: [
          { value: "0", label: "By the end of this month" },
          { value: "1", label: "By the end of next month" },
          { value: "2", label: "Within 2 months" },
          { value: "3", label: "Within 3 months" },
          { value: "6", label: "Within 6 months" },
        ],
        fallback: "",
        none: "Only when already expired",
      },
      {
        key: "REACT_MEDICATION_VALUE_SET_SELECT_DEFAULT_TAB",
        file: "frontend",
        kind: "radio",
        label: "Medicine picker opens on",
        options: [
          { value: "product", label: "Stocked products" },
          { value: "valueset", label: "Full medicine list" },
        ],
        fallback: "product",
      },
    ],
  },
  {
    id: "screens",
    title: "Forms and screens",
    summary: "Small conveniences for everyday use",
    settings: [
      {
        key: "REACT_ENABLE_QUESTIONNAIRE_DRAFT",
        file: "frontend",
        kind: "radio",
        label: "Staff can save a half-filled form as a draft",
        options: YES_NO(),
        fallback: "false",
      },
      {
        key: "REACT_MAX_FORM_DIALOG_FAVORITES",
        file: "frontend",
        kind: "int",
        label: "Forms a user can pin as favourites",
        min: 1,
        max: 50,
        fallback: "5",
      },
      {
        key: "REACT_TOAST_POSITION",
        file: "frontend",
        kind: "select",
        label: "Where pop-up messages appear",
        options: [
          { value: "top-left", label: "Top left" },
          { value: "top-center", label: "Top centre" },
          { value: "top-right", label: "Top right" },
          { value: "bottom-left", label: "Bottom left" },
          { value: "bottom-center", label: "Bottom centre" },
          { value: "bottom-right", label: "Bottom right" },
        ],
        fallback: "top-center",
      },
      {
        key: "REACT_APP_MAX_IMAGE_UPLOAD_SIZE_MB",
        file: "frontend",
        kind: "int",
        label: "Largest image staff can upload",
        unit: "MB",
        min: 1,
        max: 100,
        fallback: "2",
      },
    ],
  },
];

export const SETTINGS: Setting[] = GROUPS.flatMap((g) => g.settings);
export const SETTING_BY_KEY = new Map(SETTINGS.map((s) => [s.key, s]));

// Internal wiring and hosting settings are preserved, never editable through
// the friendly controls or the custom settings form.
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
]);
const HIDDEN_PREFIXES = [
  "POSTGRES_", "MINIO_", "BUCKET_", "DJANGO_SECURE_",
  "REACT_PUBLIC", "REACT_SENTRY", "REACT_APP_META", "REACT_PAGINATION_", "REACT_DECIMAL_",
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
