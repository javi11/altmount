import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Dedicated test config — deliberately omits the PWA/git plugins from
// vite.config.ts so the test run does not invoke git or generate a service
// worker.
export default defineConfig({
	plugins: [react()],
	test: {
		environment: "jsdom",
		globals: true,
		setupFiles: ["./src/test/setup.ts"],
		include: ["src/**/*.{test,spec}.{ts,tsx}"],
	},
});
