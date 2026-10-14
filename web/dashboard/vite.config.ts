import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import vuetify from "vite-plugin-vuetify";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [
    vue(),
    vuetify({ autoImport: true }),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    // RUNQ_API points the dev server at another runq service (e.g. a
    // seeded dev instance); default is the local service.
    proxy: {
      "/api": process.env.RUNQ_API ?? "http://localhost:8077",
      "/ws": {
        target: (process.env.RUNQ_API ?? "http://localhost:8077").replace(/^http/, "ws"),
        ws: true,
      },
    },
    hmr: true,
  },
  build: {
    outDir: "../../internal/dashboard/dist",
    emptyOutDir: true,
  },
});
