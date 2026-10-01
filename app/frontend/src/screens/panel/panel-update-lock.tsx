import {
  createContext, useCallback, useContext, useEffect, useMemo, useRef, type ReactNode,
} from "react";
import { onCareEvent } from "@/lib/bridge";

type PanelUpdateLock = {
  active: boolean;
  isActive: () => boolean;
  revision: () => number;
};

const Context = createContext<PanelUpdateLock>({ active: false, isActive: () => false, revision: () => 0 });

export function PanelUpdateLockProvider({ active, isActive, children }: Pick<PanelUpdateLock, "active" | "isActive"> & { children: ReactNode }) {
  const current = useRef(isActive);
  const version = useRef(0);
  current.current = isActive;
  useEffect(() => onCareEvent("app-update-progress", () => { version.current++; }), []);
  const read = useCallback(() => current.current(), []);
  const revision = useCallback(() => version.current, []);
  const value = useMemo(() => ({ active, isActive: read, revision }), [active, read, revision]);
  return <Context.Provider value={value}>{children}</Context.Provider>;
}

export function usePanelUpdateLock() {
  return useContext(Context);
}
