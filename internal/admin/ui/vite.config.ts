import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Builds into ../web, which the Go binary embeds. `npm run dev` proxies
// the API to a running mailer on the default admin port.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../web",
    emptyOutDir: true,
    modulePreload: { polyfill: false },
  },
  server: {
    proxy: { "/api": { target: "http://127.0.0.1:8080", changeOrigin: true } },
  },
});
