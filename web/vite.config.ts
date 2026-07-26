import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The browser only ever talks to this dev server. /api is proxied to the Go
// service so requests stay same-origin and the session cookie applies without
// any CORS configuration.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: process.env.VITE_API_PROXY_TARGET ?? "http://localhost:8080",
        changeOrigin: false,
      },
      "/healthz": {
        target: process.env.VITE_API_PROXY_TARGET ?? "http://localhost:8080",
      },
    },
  },
});
