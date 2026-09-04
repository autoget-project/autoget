import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: 8889,
  },
  plugins: [tailwindcss()],
  resolve: {
    alias: {
      daisyui: "daisyui/index.js",
    },
  },
});
