// One control per kind of setting. Each takes the raw env value (undefined when
// the key is not in the file) and reports the raw value back; the friendly
// labels live in env-schema.ts, so nothing here knows what a key means.
import { Input } from "@/components/ui/input";
import { RadioChip, RadioGroup } from "@/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import type { Option, Setting } from "./env-schema";

type ControlProps<K extends Setting["kind"]> = {
  setting: Extract<Setting, { kind: K }>;
  value: string | undefined;
  onChange: (value: string | undefined) => void;
  disabled: boolean;
  invalid?: boolean;
};

const BOX = "h-9 rounded-[8px] px-[11px] text-[13px]";

export function RadioControl({ setting, value, onChange, disabled, invalid }: ControlProps<"radio">) {
  const current = value ?? setting.fallback;
  const boolean = setting.options.length === 2 && setting.options.every((option) => /^(true|false)$/i.test(option.value));
  if (boolean && setting.options.some((option) => option.value === current)) {
    const on = current === setting.options[0].value;
    return <div className="advanced-switch-control">
      <Switch checked={on} disabled={disabled} aria-label={setting.label} aria-invalid={invalid || undefined}
        onCheckedChange={(checked) => onChange(setting.options[checked ? 0 : 1].value)} />
      <span>{setting.options[on ? 0 : 1].label}</span>
    </div>;
  }
  return (
    <RadioGroup
      value={current}
      onValueChange={onChange}
      disabled={disabled}
      aria-label={setting.label}
      aria-invalid={invalid || undefined}
      className="advanced-radio-segment"
    >
      {setting.options.map((o) => (
        <RadioChip key={o.value} value={o.value}>
          {o.label}
        </RadioChip>
      ))}
    </RadioGroup>
  );
}

// Radix Select cannot carry an empty-string item, so "not set" travels as NONE.
const NONE = "\0none";

export function SelectControl({ setting, value, onChange, disabled, invalid }: ControlProps<"select">) {
  const current = value ?? setting.fallback;
  return (
    <Select
      value={current === "" ? NONE : current}
      onValueChange={(v) => onChange(v === NONE ? "" : v)}
      disabled={disabled}
    >
      <SelectTrigger className={cn(BOX, "w-full")} aria-label={setting.label} aria-invalid={invalid || undefined}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {setting.none ? <SelectItem value={NONE}>{setting.none}</SelectItem> : null}
        {current && !setting.options.some((option) => option.value === current)
          ? <SelectItem value={current}>Current value isn't listed</SelectItem> : null}
        {setting.options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** Comma-separated list of codes shown as pressable chips, in option order. */
export function MultiControl({ setting, value, onChange, disabled, invalid }: ControlProps<"multi">) {
  const chosen = value === undefined ? setting.fallback : splitList(value);
  return (
    <ToggleGroup
      type="multiple"
      value={chosen}
      onValueChange={(next) => onChange(joinList(next, setting.options))}
      disabled={disabled}
      aria-label={setting.label}
      aria-invalid={invalid || undefined}
      className={cn("@lg:justify-end", invalid && "rounded-md ring-2 ring-danger-line")}
    >
      {setting.options.map((o) => (
        <ToggleGroupItem key={o.value} value={o.value}>
          {o.label}
        </ToggleGroupItem>
      ))}
      {chosen.filter((entry) => !setting.options.some((option) => option.value === entry)).map((entry) =>
        <ToggleGroupItem key={entry} value={entry}>Unlisted: {entry}</ToggleGroupItem>)}
    </ToggleGroup>
  );
}

export function splitList(value: string): string[] {
  return value.split(",").map((s) => s.trim()).filter(Boolean);
}

function joinList(chosen: string[], options: Option[]): string {
  return [...options.filter((o) => chosen.includes(o.value)).map((o) => o.value),
    ...chosen.filter((entry) => !options.some((option) => option.value === entry))].join(",");
}

export function IntControl({ setting, value, onChange, disabled, invalid }: ControlProps<"int">) {
  return (
    <div className="flex items-center gap-2">
      <Input
        type="number"
        inputMode="numeric"
        className={cn(BOX, "w-[120px] text-right")}
        placeholder={setting.fallback ?? ""}
        min={setting.min}
        max={setting.max}
        step={1}
        value={value ?? ""}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        aria-label={setting.label}
        aria-invalid={invalid || undefined}
      />
      {setting.unit ? (
        <span className="text-[13px] text-muted-foreground">{setting.unit}</span>
      ) : null}
    </div>
  );
}

export function TextControl({ setting, value, onChange, disabled, invalid }: ControlProps<"text">) {
  return (
    <Input
      type="text"
      className={cn(BOX, "w-full")}
      placeholder={setting.placeholder ?? ""}
      autoComplete="off"
      spellCheck={false}
      value={value ?? ""}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
      aria-label={setting.label}
      aria-invalid={invalid || undefined}
    />
  );
}

export function SettingControl(props: {
  setting: Setting;
  value: string | undefined;
  onChange: (value: string | undefined) => void;
  disabled: boolean;
  invalid?: boolean;
}) {
  const { setting } = props;
  switch (setting.kind) {
    case "radio":
      return <RadioControl {...props} setting={setting} />;
    case "select":
      return <SelectControl {...props} setting={setting} />;
    case "multi":
      return <MultiControl {...props} setting={setting} />;
    case "int":
      return <IntControl {...props} setting={setting} />;
    case "text":
      return <TextControl {...props} setting={setting} />;
  }
}
