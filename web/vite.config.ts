import { defineConfig } from "vite";

// In development the page runs on Vite and the API on a local zarf-server
// (zarf-server -addr 127.0.0.1:8080 -data data); in production the server serves both.
export default defineConfig({
  server: {
    proxy: { "/api": "http://127.0.0.1:8080" },
  },
  build: {
    outDir: "dist",
    assetsInlineLimit: 0, // fonts and the tint as files: the CSP allows only same-origin files, no data: URLs
    rollupOptions: {
      input: { main: "index.html" },
    },
  },
});
