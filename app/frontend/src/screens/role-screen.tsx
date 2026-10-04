import { ArrowRight, Clock, Wifi } from "lucide-react";
import { useRef } from "react";

import logoMark from "@/assets/care-logo-mark.svg";
import { StartUpdateCard } from "@/components/start-update-card";
import { Button } from "@/components/ui/button";
import { useAppUpdate } from "@/hooks/use-app-update";
import { useCare } from "@/state/care-store";

import "./role-screen.css";

export function RoleScreen() {
  const { selectRole, busy } = useCare();
  const navigating = useRef(false);
  const update = useAppUpdate(false, true, () => navigating.current);
  const paused = busy || update.active;
  const chooseRole = (role: "server" | "client") => {
    if (busy || navigating.current || update.isActive()) return;
    navigating.current = true;
    selectRole(role);
  };
  return (
    <div className="start-screen">
      <aside className="start-brand-panel" aria-label="CARE Clinic">
        <div className="start-brand">
          <img src={logoMark} alt="" />
          <span>CARE Clinic</span>
        </div>
        <div className="start-hero">
          <h2>Your clinic's records, running on this computer.</h2>
        </div>
        <StartUpdateCard controller={update} />
      </aside>
      <main className="start-main" aria-labelledby="start-title">
        <div className="start-body">
          <div className="start-kicker">Welcome</div>
          <h1 id="start-title">Set up CARE on this computer</h1>
          <p className="start-subtitle">This computer will run CARE for your whole clinic.</p>
          <p className="start-expectation">
            <Clock aria-hidden="true" />
            <span>Takes about 20 minutes. Internet is needed.</span>
          </p>
          <div className="start-primary-row">
            <Button
              variant="primary"
              className="start-primary"
              disabled={paused}
              aria-describedby={paused ? "start-paused" : undefined}
              onClick={() => chooseRole("server")}
            >
              Start setup
            </Button>
            {paused ? (
              <p id="start-paused" className="start-paused" role="status">
                You can continue as soon as the update has finished.
              </p>
            ) : null}
          </div>
          <div className="start-divider" aria-hidden="true"><span>or</span></div>
          <Button
            className="start-connect"
            disabled={paused}
            aria-labelledby="start-connect-title"
            aria-describedby={`start-connect-description${paused ? " start-paused" : ""}`}
            onClick={() => chooseRole("client")}
          >
            <span className="start-connect-icon"><Wifi aria-hidden="true" /></span>
            <span className="start-connect-copy">
              <strong id="start-connect-title">Connect to an existing server on the local network</strong>
              <span id="start-connect-description">CARE is already running on another computer in this clinic</span>
            </span>
            <ArrowRight aria-hidden="true" />
          </Button>
        </div>
      </main>
    </div>
  );
}
