import { useState } from "react";

import { AppUpdateCard } from "@/components/app-update-card";
import { Screen } from "@/components/screen";
import { SectionTitle } from "@/components/section-header";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { useCare } from "@/state/care-store";

export function FailedScreen() {
  const { run, retryInstall, restartSetup, busy } = useCare();
  const [retrying, setRetrying] = useState(false);

  return (
    <Screen className="px-[42px] pt-[44px] pb-[30px]">
      <div className="flex items-start gap-3.5">
        <span className="flex size-[34px] flex-none items-center justify-center rounded-full border border-danger-line bg-danger-bg font-mono text-base leading-none font-bold text-danger-ink">
          !
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-bold tracking-[-0.015em] text-ink">
            Installation failed
          </h1>
          <p className="mt-1.5 text-sm text-muted-foreground">
            Nothing was saved. Fix the issue below and try again.
          </p>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto pt-5">
        <Accordion type="single" collapsible>
          <AccordionItem value="details">
            <AccordionTrigger>
              <SectionTitle title="Technical details" summary="For support only" />
            </AccordionTrigger>
            <AccordionContent>
              <pre className="m-0 max-h-[260px] overflow-auto rounded-lg bg-[#0b1f17] px-4 py-3.5 font-mono text-[12.5px] leading-[1.6] break-words whitespace-pre-wrap text-[#d7f7e6]">
                {run.failMessage}
              </pre>
            </AccordionContent>
          </AccordionItem>
        </Accordion>
        <div className="mt-4">
          <AppUpdateCard disabled={retrying} />
        </div>
      </div>

      <div className="flex items-center gap-3.5">
        <Button
          variant="primary"
          size="lg"
          className="shadow-lift disabled:shadow-none"
          disabled={retrying || busy}
          onClick={() => {
            setRetrying(true);
            void retryInstall().finally(() => setRetrying(false));
          }}
        >
          Try again
        </Button>
        <Button disabled={retrying || busy} onClick={restartSetup}>Back to setup</Button>
      </div>
    </Screen>
  );
}
