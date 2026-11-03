import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Each dashboard is built from its own folder and the parts in shared/:
// `vite build --mode admin` builds admin/ into ../admin/web, the copy the
// Go binary embeds. `npm run dev:admin` proxies the API to a running
// mailer.
const apps: Record<string, { out: string; api: string }> = {
  admin: { out: "../../admin/web", api: "http://127.0.0.1:8080" },
};

export default defineConfig(({ mode }) => {
  const app = apps[mode] ? mode : "admin";
  return {
    root: app,
    publicDir: "../shared/public",
    plugins: [react()],
    build: {
      outDir: apps[app].out,
      emptyOutDir: true,
      modulePreload: { polyfill: false },
    },
    server: {
      // The admin's Help pages import the guides from the repository's docs/.
      fs: { allow: ["..", "../../../docs"] },
      proxy: { "/api": { target: apps[app].api, changeOrigin: true } },
    },
  };
});
