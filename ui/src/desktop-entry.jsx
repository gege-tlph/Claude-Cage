import React, { lazy, Suspense } from "react";
import { createRoot } from "react-dom/client";
import { installDiagnostics } from "./diagnostics.mjs";
import { DiagnosticBoundary } from "./diagnostics.jsx";
import { ClientRoot } from "./remote-hosts";
import { native } from "./api";
import "./style.css";
import { IconHints } from "./icon-hints.jsx";
import { useTranslation } from "./interface-language.mjs";
import { StartupSplash, StartupStage } from "./startup-splash.jsx";

const Workbench = lazy(() => import("./main.jsx"));

function WorkbenchLoading() {
  const tr = useTranslation();
  return <StartupStage status={tr("正在打开工作台…")} />;
}

installDiagnostics();
// Keep the native menu usable while the workbench is loading or disconnected.
window.cageNativeAction = (command) => {
  if (window.cageWorkbenchAction) return window.cageWorkbenchAction(command);
  if (command === "quit") return native("quitNow");
};
createRoot(document.getElementById("root")).render(
  <DiagnosticBoundary>
    <IconHints />
    <StartupSplash />
    <ClientRoot>
      <Suspense
        fallback={<WorkbenchLoading />}
      >
        <Workbench />
      </Suspense>
    </ClientRoot>
  </DiagnosticBoundary>,
);

window.addEventListener("hashchange", () => {
  if (location.hash) location.reload();
});
