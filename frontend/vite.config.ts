import { execSync } from "child_process";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

function getGitCommit(): string {
	if (process.env.GIT_COMMIT) return process.env.GIT_COMMIT;
	try {
		return execSync("git rev-parse --short HEAD").toString().trim();
	} catch {
		return "unknown";
	}
}

function getAppVersion(): string {
	if (process.env.APP_VERSION) return process.env.APP_VERSION;
	try {
		return execSync("git describe --tags --always --dirty").toString().trim();
	} catch {
		return process.env.npm_package_version || "0.0.0";
	}
}

// https://vite.dev/config/
export default defineConfig({
	plugins: [react(), tailwindcss()],
	define: {
		__APP_VERSION__: JSON.stringify(getAppVersion()),
		__GIT_COMMIT__: JSON.stringify(getGitCommit()),
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
