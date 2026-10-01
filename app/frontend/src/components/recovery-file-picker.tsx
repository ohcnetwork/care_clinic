import { useState } from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";

export function RecoveryFilePicker({
  value, onChange, disabled,
}: {
  value: string;
  onChange: (path: string) => void;
  disabled: boolean;
}) {
  const [problem, setProblem] = useState("");
  const [choosing, setChoosing] = useState(false);
  const choose = async () => {
    setChoosing(true);
    setProblem("");
    try {
      const path = await bridge.ChooseRecoveryFile();
      if (path) onChange(path);
    } catch (e) {
      setProblem(errorText(e));
    } finally {
      setChoosing(false);
    }
  };
  return (
    <div className="flex flex-col gap-2 text-[13px]">
      <div className="flex items-center gap-3">
        <Button disabled={disabled || choosing} onClick={() => void choose()}>
          Choose backup recovery file
        </Button>
        <span className="min-w-0 flex-1 truncate text-muted-foreground" title={value}>
          {value || "Select the file saved when this backup's clinic was set up."}
        </span>
      </div>
      {problem ? <Alert variant="danger">{problem}</Alert> : null}
    </div>
  );
}
