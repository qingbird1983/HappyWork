import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// No wails bindings plugin: the frontend talks to services via
// Call.ByName strings (design.md §2), so generated bindings are optional.
// emptyOutDir=false keeps the committed dist/.gitkeep alive (the embed in
// frontend/embed.go needs a non-empty dist to compile on fresh clones);
// stale hashed assets accumulate across rebuilds, which is fine at Phase 1 scale.
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: {
    emptyOutDir: false,
  },
  plugins: [react()],
});
