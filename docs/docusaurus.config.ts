import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";
import webpack from "webpack";

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
	title: "AltMount Documentation",
	tagline: "WebDAV server backed by NZB/Usenet",
	favicon: "img/favicon.ico",

	// Future flags, see https://docusaurus.io/docs/api/docusaurus-config#future
	future: {
		v4: true, // Improve compatibility with the upcoming Docusaurus v4
	},

	// Set the production url of your site here
	url: "https://altmount.kipsilabs.top",
	// Set the /<baseUrl>/ pathname under which your site is served
	// For GitHub pages deployment, it is often '/<projectName>/'
	baseUrl: "/",

	// GitHub pages deployment config.
	// If you aren't using GitHub pages, you don't need these.
	organizationName: "javi11", // Usually your GitHub org/user name.
	projectName: "altmount", // Usually your repo name.

	onBrokenLinks: "throw",
	onBrokenMarkdownLinks: "warn",

	// Even if you don't use internationalization, you can use this field to set
	// useful metadata like html lang. For example, if your site is Chinese, you
	// may want to replace "en" with "zh-Hans".
	i18n: {
		defaultLocale: "en",
		locales: ["en"],
	},

	presets: [
		[
			"classic",
			{
				docs: {
					sidebarPath: "./sidebars.ts",
					// Please change this to your repo.
					// Remove this to remove the "edit this page" links.
					editUrl: "https://github.com/javi11/altmount/tree/main/docs/",
				},
				blog: false,
				theme: {
					customCss: "./src/css/custom.css",
				},
			} satisfies Preset.Options,
		],
	],

	themes: ["@docusaurus/theme-mermaid"],

	plugins: [
		function webpackPolyfillPlugin() {
			return {
				name: "webpack-buffer-polyfill",
				configureWebpack() {
					return {
						resolve: {
							fallback: {
								stream: false,
								buffer: require.resolve("buffer/"),
							},
						},
						plugins: [
							new webpack.ProvidePlugin({
								Buffer: ["buffer", "Buffer"],
							}),
						],
					};
				},
			};
		},
	],

	themeConfig: {
		navbar: {
			title: "AltMount",
			logo: {
				alt: "AltMount Logo",
				src: "img/logo.png",
			},
			items: [
				{
					type: "docSidebar",
					sidebarId: "tutorialSidebar",
					position: "left",
					label: "Documentation",
				},
				{
					to: "/api-explorer",
					label: "API Explorer",
					position: "left",
				},
				{
					href: "https://github.com/javi11/altmount",
					label: "GitHub",
					position: "right",
				},
			],
		},
		footer: {
			style: "dark",
			links: [
				{
					title: "Documentation",
					items: [
						{
							label: "Getting Started",
							to: "/docs/intro",
						},
						{
							label: "Installation",
							to: "/docs/Installation/other-methods",
						},
					],
				},
				{
					title: "Community",
					items: [
						{
							label: "GitHub Issues",
							href: "https://github.com/javi11/altmount/issues",
						},
						{
							label: "GitHub Discussions",
							href: "https://github.com/javi11/altmount/discussions",
						},
					],
				},
				{
					title: "More",
					items: [
						{
							label: "GitHub",
							href: "https://github.com/javi11/altmount",
						},
						{
							label: "Docker Hub",
							href: "https://hub.docker.com/r/javi11/altmount",
						},
					],
				},
			],
			copyright: `Copyright © ${new Date().getFullYear()} AltMount. Built with Docusaurus.`,
		},
		metadata: [
			{ name: 'description', content: 'AltMount is a high-performance WebDAV server backed by Usenet with streaming, ARR integration, rclone support, and native FUSE mounting.' },
			{ property: 'og:type', content: 'website' },
			{ property: 'og:site_name', content: 'AltMount Documentation' },
			{ property: 'og:description', content: 'Mount Usenet as a local drive. Stream media directly, integrate with Sonarr/Radarr, and use rclone — all in a single binary.' },
			{ name: 'twitter:card', content: 'summary' },
			{ name: 'twitter:title', content: 'AltMount Documentation' },
			{ name: 'twitter:description', content: 'Mount Usenet as a local drive. Stream media directly, integrate with Sonarr/Radarr, and use rclone — all in a single binary.' },
			{ name: 'keywords', content: 'altmount, usenet, webdav, nzb, nntp, sabnzbd, sonarr, radarr, streaming, rclone, fuse, mount, docker' },
		],
		prism: {
			theme: prismThemes.github,
			darkTheme: prismThemes.dracula,
			additionalLanguages: ["bash", "yaml", "json", "docker"],
		},
		mermaid: {
			theme: { light: "neutral", dark: "dark" },
		},
	} satisfies Preset.ThemeConfig,
};

export default config;
