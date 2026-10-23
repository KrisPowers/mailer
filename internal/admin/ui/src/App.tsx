import { useEffect, useRef, useState } from "react";
import { FloatingProvider, useFloating } from "./components/Floating";
import { ModalProvider, useModal } from "./components/Modal";
import { ToastProvider } from "./components/Toasts";
import { OverviewProvider, setupStep, useOverview } from "./overview";
import { useView, type View } from "./router";
import { Wizard } from "./setup/Wizard";
import { SetupBanner } from "./shell/SetupBanner";
import { Sidebar } from "./shell/Sidebar";
import { Topbar } from "./shell/Topbar";
import { Accounts } from "./views/Accounts";
import { Aliases } from "./views/Aliases";
import { Dashboard } from "./views/Dashboard";
import { Logs } from "./views/Logs";
import { Queue } from "./views/Queue";
import { Settings } from "./views/Settings";

const PAGES: Record<View, () => React.JSX.Element> = {
  dashboard: Dashboard,
  accounts: Accounts,
  aliases: Aliases,
  queue: Queue,
  logs: Logs,
  settings: Settings,
};

// Providers are nested so dialogs and menus can use overview data and toasts.
export function App() {
  return (
    <ToastProvider>
      <OverviewProvider>
        <ModalProvider>
          <FloatingProvider>
            <Shell />
          </FloatingProvider>
        </ModalProvider>
      </OverviewProvider>
    </ToastProvider>
  );
}

function Shell() {
  const view = useView();
  const { ov, error } = useOverview();
  const { open } = useModal();
  const { close: closeFloating } = useFloating();
  const [navOpen, setNavOpen] = useState(false);
  const autoOpened = useRef(false);

  useEffect(() => {
    setNavOpen(false);
    closeFloating();
  }, [view, closeFloating]);

  // Open the wizard once per browser session while nothing is set up.
  useEffect(() => {
    if (!ov || autoOpened.current) return;
    autoOpened.current = true;
    let shown = false;
    try {
      shown = sessionStorage.getItem("mailer.wiz") === "1";
      sessionStorage.setItem("mailer.wiz", "1");
    } catch {
      // storage unavailable
    }
    if (setupStep(ov) === 1 && !shown) open(<Wizard />);
  }, [ov, open]);

  const Page = PAGES[view];
  return (
    <div className={"shell" + (navOpen ? " nav-open" : "")}>
      <Topbar onToggleNav={() => setNavOpen(!navOpen)} />
      <Sidebar view={view} />
      <main className="content">
        {error && !ov && <div className="banner bad"><span>Can't reach the mail server: {error}</span></div>}
        <SetupBanner />
        <div className="view"><Page key={view} /></div>
      </main>
    </div>
  );
}
