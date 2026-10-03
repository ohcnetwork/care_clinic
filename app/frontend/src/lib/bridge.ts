// The single door to Go. Everything the UI can ask the host to do goes through
// `bridge`; everything the host tells us arrives on one of CareEvent.
//
// Nothing here touches `window.go` at import time. The Wails runtime injects the
// bindings before our module runs in a packaged build, but under `wails dev` they
// can land a tick later — and reading them at module scope turns that race into a
// blank window with no diagnostics, because the throw kills the whole module
// graph before React mounts. Resolving per call costs nothing and cannot.
type CareBridge = Window["go"]["main"]["App"];

const READY_TIMEOUT_MS = 10_000;
const POLL_MS = 25;

let cached: CareBridge | null = null;

function current(): CareBridge | null {
  return window.go?.main?.App ?? null;
}

function whenReady(): Promise<CareBridge> {
  if (cached) return Promise.resolve(cached);
  const now = current();
  if (now) {
    cached = now;
    return Promise.resolve(now);
  }
  return new Promise((resolve, reject) => {
    const startedAt = Date.now();
    const poll = () => {
      const app = current();
      if (app) {
        cached = app;
        resolve(app);
        return;
      }
      if (Date.now() - startedAt > READY_TIMEOUT_MS) {
        reject(new Error("The CARE Desktop runtime is not available."));
        return;
      }
      window.setTimeout(poll, POLL_MS);
    };
    poll();
  });
}

type Methods = Record<string, (...args: unknown[]) => Promise<unknown>>;

export const bridge = new Proxy({} as CareBridge, {
  get(_target, method) {
    if (typeof method !== "string") return undefined;
    return async (...args: unknown[]) => {
      const app = await whenReady();
      const fn = (app as unknown as Methods)[method];
      // Name the method. Without this a binding the Go side no longer exports —
      // a version skew between the app and this bundle — fails as
      // "(intermediate value)[i] is not a function", which says nothing.
      if (typeof fn !== "function") {
        throw new Error(`The CARE Desktop runtime has no "${method}" method.`);
      }
      return fn.apply(app, args);
    };
  },
}) as CareBridge;

export type CareEvent =
  | "care-log"
  | "care-done"
  | "care-error"
  | "setup-done"
  | "setup-failed"
  | "uninstalled"
  | "care-update"
  | "care-check"
  | "care-storage"
  | "app-update-progress"
  | "prereq-download-progress"
  | "client-connect-progress"
  | "quit-requested"
  | "confirmation-requested"
  | "confirmation-cancelled";

/** Subscribe to a Wails event, waiting out a runtime that isn't injected yet. */
export function onCareEvent(
  event: CareEvent,
  handler: (...data: any[]) => void,
): () => void {
  let off: (() => void) | null = null;
  let cancelled = false;

  const attach = () => {
    if (cancelled) return;
    if (!window.runtime) {
      window.setTimeout(attach, POLL_MS);
      return;
    }
    off = window.runtime.EventsOn(event, handler);
  };
  attach();

  return () => {
    cancelled = true;
    off?.();
  };
}

/**
 * Write one line to the host's log file.
 *
 * Only for lines that ORIGINATE here. Anything arriving on `care-log` was written
 * to the file by Go before it was emitted, so sending it back would duplicate
 * every line of every docker build.
 */
export function logToHost(line: string): void {
  try {
    window.runtime?.LogPrint(line);
  } catch {
    /* the runtime may not be injected yet; a lost log line is never worth throwing over */
  }
}
