import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// No wails bindings plugin: the frontend talks to services via
// Call.ByName strings (design.md §2), so generated bindings are optional.
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [react()],
});
