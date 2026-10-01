import { Toaster as Sonner, toast } from "sonner";

// The design's toast: one centred pill, gone after a couple of seconds.
// `unstyled` drops sonner's card styling and keeps only its stack/positioning;
// the pill is drawn on the inner content element so it hugs its text inside the
// fixed-width, centred slot sonner positions for us.
function Toaster() {
  return (
    <Sonner
      position="bottom-center"
      duration={2600}
      offset={26}
      style={{ ["--width" as string]: "520px" }}
      toastOptions={{
        unstyled: true,
        classNames: {
          toast: "group flex w-full justify-center",
          content:
            "rounded-lg bg-brand-ink px-[18px] py-3 text-center font-sans text-[13.5px] text-[#e3fbf0] shadow-toast group-data-[type=error]:bg-danger-dark group-data-[type=error]:text-white",
        },
      }}
    />
  );
}

export { Toaster, toast };
