import { configDefaults, defineConfig } from "vitest/config"
import react from "@vitejs/plugin-react-swc"
import tailwindcss from "@tailwindcss/vite"
import path from "node:path"

const target = process.env.GHR_DEV_URL ?? "http://localhost:8080"

// Tests that need local time are named *.tz.test.ts. They run only when
// GHR_TEST_TZ names their zone, and then nothing else runs.
const zone = process.env.GHR_TEST_TZ
const zoneTests = "src/**/*.tz.test.{ts,tsx}"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  // The CSP has no font-src, so a font inlined as a data: URI is blocked.
  build: { assetsInlineLimit: 0 },
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
    env: { TZ: zone ?? "UTC" },
    setupFiles: ["./src/test/setup.ts"],
    include: [zone ? zoneTests : "src/**/*.test.{ts,tsx}"],
    exclude: zone ? configDefaults.exclude : [...configDefaults.exclude, zoneTests],
    // On a busy host a worker per CPU starves the tests, and they run past
    // Vitest's 5-second default.
    testTimeout: 15_000,
    maxWorkers: "50%",
    // Node 25 turns on its own Web Storage and warns in every worker that it
    // has no file; the tests use jsdom's localStorage.
    execArgv: ["--no-experimental-webstorage"],
  },
})
