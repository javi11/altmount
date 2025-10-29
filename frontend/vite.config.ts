import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// https://vite.dev/config/
export default defineConfig({
	plugins: [react(), tailwindcss()],
	define: {
		__APP_VERSION__: JSON.stringify(process.env.npm_package_version || "0.0.0"),
		__GIT_COMMIT__: JSON.stringify(process.env.GIT_COMMIT || "unknown"),
		__GITHUB_URL__: JSON.stringify("https://github.com/javi11/altmount"),
	},
	server: {
		port: 5173,
		strictPort: true,
		proxy: {
			"/api": {
				target: "http://localhost:8080",
				changeOrigin: true,
				ws: true,
			},
			"/sabnzbd": {
				target: "http://localhost:8080",
				changeOrigin: true,
			},
			"/webdav": {
				target: "http://localhost:8080",
				changeOrigin: true,
			},
		},
	},
});
