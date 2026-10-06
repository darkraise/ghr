import { defineConfig } from "vitest/config"
import react from "@vitejs/plugin-react-swc"
import tailwindcss from "@tailwindcss/vite"
import path from "node:path"

const target = process.env.GHR_DEV_URL ?? "http://localhost:8080"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  server: {
    host: "localhost",
    port: 5173,
    // changeOrigin stays false: the daemon must see Host: localhost so its
    // Host allowlist and Origin check pass for the dev server.
    proxy: { "/api": { target }, "/auth": { target } },
  },
  test: {
    environment: "jsdom",
    environmentOptions: { jsdom: { url: "http://localhost/" } },
    env: { TZ: "UTC" },
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
})
