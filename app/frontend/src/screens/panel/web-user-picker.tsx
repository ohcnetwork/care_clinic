import { ChevronDown, ShieldCheck, UserRound } from "lucide-react";
import { useId, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import { Input } from "@/components/ui/input";
import type { WebUser } from "@/types";

export type WebUserList =
  | { status: "loading" }
  | { status: "ready"; users: WebUser[] }
  | { status: "failed"; message: string };

type Props = {
  id: string;
  value: string;
  onChange: (username: string) => void;
  list: WebUserList;
  disabled: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const matches = (user: WebUser, query: string) => {
  const q = query.trim().toLowerCase();
  return !q || user.username.toLowerCase().includes(q) || user.name.toLowerCase().includes(q);
};

// A username field that doubles as a searchable list of CARE staff. Typing a
// username by hand still works when the list can't be loaded.
export function WebUserPicker({ id, value, onChange, list, disabled, open, onOpenChange }: Props) {
  const listId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [filtering, setFiltering] = useState(false);
  const [active, setActive] = useState(0);
  const users = list.status === "ready" ? list.users : [];
  const shown = useMemo(() => (filtering ? users.filter((user) => matches(user, value)) : users), [users, filtering, value]);
  const selected = users.find((user) => user.username === value.trim());
  const expanded = open && list.status === "ready" && !disabled;

  const show = (filter: boolean) => {
    if (list.status !== "ready" || disabled) return;
    setFiltering(filter);
    const index = filter ? 0 : Math.max(0, users.findIndex((user) => user.username === value.trim()));
    setActive(index);
    onOpenChange(true);
  };
  const choose = (user: WebUser) => {
    onChange(user.username);
    onOpenChange(false);
    inputRef.current?.focus();
  };
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!expanded) return show(false);
      const step = event.key === "ArrowDown" ? 1 : -1;
      setActive((index) => (shown.length ? (index + step + shown.length) % shown.length : 0));
    } else if (event.key === "Enter" && expanded && shown[active]) {
      event.preventDefault();
      choose(shown[active]);
    } else if (event.key === "Tab" && expanded) {
      onOpenChange(false);
    }
  };

  let hint: ReactNode;
  if (list.status === "loading") hint = "Loading CARE staff…";
  else if (list.status === "failed") hint = <>{list.message} Type the username instead. Capital letters matter.</>;
  else if (selected && !selected.active) hint = <span className="advanced-field-error">
    This account is deactivated in CARE. A new password won't let them sign in until it is reactivated.
  </span>;
  else hint = "Pick someone from the list, or type the username they sign in with. Capital letters matter.";

  return <div className="advanced-field web-user-picker">
    <label htmlFor={id}>CARE username</label>
    <div className="advanced-secret">
      <UserRound aria-hidden="true" />
      <Input id={id} ref={inputRef} value={value} disabled={disabled}
        role="combobox" aria-expanded={expanded} aria-controls={listId} aria-autocomplete="list"
        aria-activedescendant={expanded && shown[active] ? `${listId}-${active}` : undefined}
        onChange={(event) => { onChange(event.target.value); show(true); }}
        onKeyDown={onKeyDown}
        onBlur={() => onOpenChange(false)}
        autoComplete="off" spellCheck={false} autoCapitalize="off" autoFocus />
      <button type="button" className="web-user-toggle" aria-label={expanded ? "Hide CARE staff" : "Show CARE staff"}
        disabled={disabled || list.status !== "ready"} tabIndex={-1}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => { if (expanded) onOpenChange(false); else { show(false); inputRef.current?.focus(); } }}>
        <ChevronDown aria-hidden="true" />
      </button>
    </div>
    {expanded ? <ul className="web-user-list" id={listId} role="listbox" aria-label="CARE staff">
      {shown.length === 0 ? <li className="web-user-empty" role="presentation">No CARE staff match “{value.trim()}”.</li> : null}
      {shown.map((user, index) => (
        <li key={user.username} id={`${listId}-${index}`} role="option"
          aria-selected={user.username === value.trim()}
          className={index === active ? "is-active" : undefined}
          onMouseDown={(event) => event.preventDefault()}
          onMouseEnter={() => setActive(index)}
          onClick={() => choose(user)}>
          <span className="web-user-text">
            <span className="web-user-name">{user.name || user.username}</span>
            {user.name ? <span className="web-user-username">{user.username}</span> : null}
          </span>
          {user.superuser ? <span className="web-user-badge"><ShieldCheck aria-hidden="true" />Admin</span> : null}
          {!user.active ? <span className="web-user-badge is-muted">Inactive</span> : null}
        </li>
      ))}
    </ul> : null}
    <div className="advanced-field-hint">{hint}</div>
  </div>;
}
