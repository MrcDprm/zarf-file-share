import { defineConfig } from "vite";

const API = "http://127.0.0.1:8080";

// In development the page runs on Vite and the API on a local zarf-server
// (zarf-server -addr 127.0.0.1:8080 -data data); in production the server serves both.
export default defineConfig({
  plugins: [
    {
      // /d/<id> receives and /r/<rid> uploads to a request, as the Go server routes them in production.
      name: "zarf-page-routes",
      configureServer(server) {
        server.middlewares.use((req, _res, next) => {
          if (req.url && /^\/d\/[A-Za-z0-9_-]+\/?(\?.*)?$/.test(req.url)) req.url = "/d.html";
          else if (req.url && /^\/r\/[A-Za-z0-9_-]+\/?(\?.*)?$/.test(req.url)) req.url = "/r.html";
          next();
        });
      },
    },
  ],
  server: {
    proxy: {
      "/api": {
        target: API,
        ws: true,
        changeOrigin: true,
        // The server accepts WebSockets only from its own pages; in development the page
        // comes from Vite, so the proxy presents it (Host and Origin) as the server's own.
        configure: (proxy) => proxy.on("proxyReqWs", (req) => req.setHeader("Origin", API)),
      },
    },
  },
  build: {
    outDir: "dist",
    assetsInlineLimit: 0, // fonts and the tint as files: the CSP allows only same-origin files, no data: URLs
    rollupOptions: {
      input: { main: "index.html", receive: "d.html", give: "r.html" },
    },
  },
});
