import { createRoot } from "react-dom/client";
import { api } from "./api";
import { App } from "./App";
import "./styles.css";

// Inside the Mailer app window, web links open in the default browser.
if (location.hostname === "wails.localhost" || location.protocol === "wails:") {
  document.addEventListener("click", (e) => {
    const a = (e.target as Element).closest<HTMLAnchorElement>("a[href^='https://']");
    if (!a) return;
    e.preventDefault();
    api.openURL(a.href);
  });
}

createRoot(document.getElementById("root")!).render(<App />);
