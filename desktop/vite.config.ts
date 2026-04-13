import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
  ],

  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },

  // Vite dev server settings — Tauri expects port 5173 by default.
  server: {
    port: 5173,
    strictPort: true,
    // Allow connections from Tauri's WebView.
    host: "localhost",
  },

  // Production build output — matches tauri.conf.json > build > frontendDist.
  build: {
    outDir: "dist",
    // Tauri uses ES modules.
    target: ["es2022", "chrome105"],
    // Smaller chunks for faster WebView load.
    chunkSizeWarningLimit: 500,
  },
});
