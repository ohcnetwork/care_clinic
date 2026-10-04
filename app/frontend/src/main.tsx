// CARE Clinic control app — installer + control panel, driven by the Go bridge
// (window.go.main.App) and Wails events. Fonts are bundled locally so the app
// works with no network; icons come from lucide-react as inline SVG, so there is
// no icon font to ship either.
import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-sans/700.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/ibm-plex-mono/600.css";
import "./index.css";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "@/App";
import { ErrorBoundary } from "@/components/error-boundary";
import { Toaster } from "@/components/ui/sonner";
import { CareProvider } from "@/state/care-store";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ErrorBoundary>
      <CareProvider>
        <App />
        <Toaster />
      </CareProvider>
    </ErrorBoundary>
  </StrictMode>,
);
