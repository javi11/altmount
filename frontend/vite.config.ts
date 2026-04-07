import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { execSync } from "child_process";
import { defineConfig } from "vite";
import { VitePWA } from "vite-plugin-pwa";

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
	plugins: [
		react(),
		tailwindcss(),
		VitePWA({
			registerType: "prompt",
			includeAssets: ["favicon.ico", "logo.png", "apple-touch-icon-180x180.png"],
			manifest: {
				name: "AltMount",
				short_name: "AltMount",
				description: "A NZB mounting application",
				theme_color: "#1d1d1d",
				background_color: "#1d1d1d",
				display: "standalone",
				start_url: "/",
				icons: [
					{
						src: "pwa-64x64.png",
						sizes: "64x64",
						type: "image/png",
					},
					{
						src: "pwa-192x192.png",
						sizes: "192x192",
						type: "image/png",
					},
					{
						src: "pwa-512x512.png",
						sizes: "512x512",
						type: "image/png",
					},
					{
						src: "maskable-icon-512x512.png",
						sizes: "512x512",
						type: "image/png",
						purpose: "maskable",
					},
				],
			},
			workbox: {
				globPatterns: ["**/*.{js,css,html,ico,png,svg,woff2}"],
				runtimeCaching: [
					{
						urlPattern: /^\/api\/.*/i,
						handler: "NetworkFirst",
						options: {
							cacheName: "api-cache",
							expiration: {
								maxEntries: 50,
								maxAgeSeconds: 60 * 5,
							},
							networkTimeoutSeconds: 5,
						},
					},
				],
				navigateFallback: "index.html",
				navigateFallbackDenylist: [/^\/webdav/, /^\/sabnzbd/],
			},
		}),
	],
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
