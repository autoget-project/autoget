import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: 8889,
  },
  plugins: [tailwindcss()],
  test: {
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      exclude: ["src/**/*.test.ts", "src/themes/**"],
      reporter: ["text-summary", "lcov"],
    },
  },
  resolve: {
    alias: {
      daisyui: "daisyui/index.js",
    },
  },
});
