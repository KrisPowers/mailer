import { useEffect, useRef, useState } from "react";
import { Setup } from "./flows/Setup";
import { Gate } from "./gate/Gate";
import { OverviewProvider, setupStep, useOverview } from "./overview";
import { Access } from "./pages/Access";
import { Accounts } from "./pages/Accounts";
import { Docs } from "./pages/Docs";
import { Logs } from "./pages/Logs";
import { Queue } from "./pages/Queue";
import { Settings } from "./pages/Settings";
import { useRoute } from "./router";
import { Header } from "./shell/Header";
import { Sidebar } from "./shell/Sidebar";
import { StatusBar } from "./shell/Statusbar";
import { FloatingProvider, useFloating } from "./ui/Floating";
import { Icon } from "./ui/Icon";
import { ModalProvider, useModal } from "./ui/Modal";
import { ToastProvider } from "./ui/Toast";
import { TooltipLayer } from "./ui/Tooltip";

// Nothing past the Gate loads until this device is approved and someone
// has signed in on it.
export function App() {
  return (
    <ToastProvider>
      <Gate>
        <OverviewProvider>
          <FloatingProvider>
            <ModalProvider>
              <Shell />
            </ModalProvider>
          </FloatingProvider>
        </OverviewProvider>
      </Gate>
      <TooltipLayer />
    </ToastProvider>
  );
}

function Shell() {
  const route = useRoute();
  const { ov, error } = useOverview();
  const { open } = useModal();
  const { close: closeFloating } = useFloating();
  const [navOpen, setNavOpen] = useState(false);
  const autoOpened = useRef(false);
  const main = useRef<HTMLElement>(null);

  useEffect(() => {
    setNavOpen(false);
    closeFloating();
    main.current?.scrollTo(0, 0);
  }, [route.view, route.param, closeFloating]);

  // Open setup once per browser session while nothing is set up.
  useEffect(() => {
    if (!ov || autoOpened.current) return;
    autoOpened.current = true;
    let shown = false;
    try {
      shown = sessionStorage.getItem("mailer.setup") === "1";
      sessionStorage.setItem("mailer.setup", "1");
    } catch {
      // storage unavailable
    }
    if (setupStep(ov) === 1 && !shown) open(<Setup />);
  }, [ov, open]);

  const page = {
    access: <Access />,
    accounts: <Accounts param={route.param} />,
    queue: <Queue />,
    logs: <Logs />,
    docs: <Docs param={route.param} />,
    settings: <Settings />,
  }[route.view];

  return (
    <div className={"app" + (navOpen ? " nav-open" : "")}>
      <a className="skip" href="#main">Skip to content</a>
      <Header onToggleNav={() => setNavOpen(!navOpen)} />
      <Sidebar view={route.view} />
      <main className="main" id="main" ref={main} tabIndex={-1}>
        {error && !ov && <div className="callout bad"><Icon name="error" /><span>Can't reach the mail server: {error}</span></div>}
        <div className="page" key={route.view + route.param}>{page}</div>
      </main>
      <StatusBar />
    </div>
  );
}
