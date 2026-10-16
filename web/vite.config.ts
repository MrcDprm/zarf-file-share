import { defineConfig } from "vite";

// In development the page runs on Vite and the API on a local zarf-server
// (zarf-server -addr 127.0.0.1:8080 -data data); in production the server serves both.
export default defineConfig({
  plugins: [
    {
      // /d/<id> is the receiving page, as the Go server routes it in production.
      name: "zarf-receive-route",
      configureServer(server) {
        server.middlewares.use((req, _res, next) => {
          if (req.url && /^\/d\/[A-Za-z0-9_-]+\/?(\?.*)?$/.test(req.url)) req.url = "/d.html";
          next();
        });
      },
    },
  ],
  server: {
    proxy: { "/api": "http://127.0.0.1:8080" },
  },
  build: {
    outDir: "dist",
    assetsInlineLimit: 0, // fonts and the tint as files: the CSP allows only same-origin files, no data: URLs
    rollupOptions: {
      input: { main: "index.html", receive: "d.html" },
    },
  },
});
