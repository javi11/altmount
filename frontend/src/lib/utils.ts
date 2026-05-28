import { type ClassValue, clsx } from "clsx";

export function cn(...inputs: ClassValue[]) {
	return clsx(inputs);
}

export function formatBytes(bytes: number, decimals = 2) {
	if (bytes === 0) return "0 Bytes";

	const k = 1024;
	const dm = decimals < 0 ? 0 : decimals;
	const sizes = ["Bytes", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"];

	const i = Math.floor(Math.log(bytes) / Math.log(k));

	return `${Number.parseFloat((bytes / k ** i).toFixed(dm))} ${sizes[i]}`;
}

export function formatSpeed(bytesPerSec: number) {
	if (bytesPerSec === 0) return "0 B/s";
	const units = ["B/s", "KB/s", "MB/s", "GB/s"];
	const index = Math.floor(Math.log(bytesPerSec) / Math.log(1024));
	const value = bytesPerSec / 1024 ** index;
	return `${value.toFixed(1)} ${units[index]}`;
}

export function formatDuration(seconds: number) {
	let s = seconds;

	if (s >= 86400) {
		const days = Math.floor(s / 86400);
		const hours = Math.floor((s % 86400) / 3600);
		return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
	}

	if (s >= 3600) {
		const hours = Math.floor(s / 3600);
		const minutes = Math.floor((s % 3600) / 60);
		return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
	}

	const units = [
		{ label: "m", seconds: 60 },
		{ label: "s", seconds: 1 },
	];

	const parts: string[] = [];

	for (const unit of units) {
		const count = Math.floor(s / unit.seconds);
		if (count > 0) {
			parts.push(`${count}${unit.label}`);
			s -= count * unit.seconds;
		}
	}

	return parts.length > 0 ? parts.join(" ") : "0s";
}

export function formatRelativeTime(date: string | Date) {
	const now = new Date();
	const target = new Date(date);
	const diffInSeconds = Math.floor((now.getTime() - target.getTime()) / 1000);

	if (diffInSeconds < 60) return "just now";
	if (diffInSeconds < 3600) return `${Math.floor(diffInSeconds / 60)}m ago`;
	if (diffInSeconds < 86400) return `${Math.floor(diffInSeconds / 3600)}h ago`;
	if (diffInSeconds < 2592000) return `${Math.floor(diffInSeconds / 86400)}d ago`;

	return target.toLocaleDateString();
}

export function formatFutureTime(date: string | Date | null | undefined): string {
	if (!date) return "Never";

	const now = new Date();
	const target = new Date(date);
	const diffInSeconds = Math.floor((target.getTime() - now.getTime()) / 1000);

	// If the date is in the past, return "Now"
	if (diffInSeconds <= 0) return "Now";

	if (diffInSeconds < 60) return "in <1m";
	if (diffInSeconds < 3600) return `in ${Math.floor(diffInSeconds / 60)}m`;
	if (diffInSeconds < 86400) {
		const hours = Math.floor(diffInSeconds / 3600);
		const minutes = Math.floor((diffInSeconds % 3600) / 60);
		return minutes > 0 ? `in ${hours}h ${minutes}m` : `in ${hours}h`;
	}
	if (diffInSeconds < 2592000) {
		const days = Math.floor(diffInSeconds / 86400);
		const hours = Math.floor((diffInSeconds % 86400) / 3600);
		return hours > 0 ? `in ${days}d ${hours}h` : `in ${days}d`;
	}

	return `on ${target.toLocaleDateString()}`;
}

export function getStatusColor(status: string): string {
	switch (status.toLowerCase()) {
		case "completed":
		case "healthy":
			return "success";
		case "processing":
		case "checking":
		case "repair_triggered":
			return "info";
		case "pending":
		case "masked":
			return "warning";
		case "failed":
		case "corrupted":
		case "unhealthy":
			return "error";
		case "degraded":
			return "warning";
		default:
			return "neutral";
	}
}

export function truncateText(text: string, maxLength = 50): string {
	if (!text) return "";
	if (text.length <= maxLength) return text;
	return `${text.slice(0, maxLength)}...`;
}

export function debounce<T extends (...args: unknown[]) => unknown>(
	func: T,
	wait: number,
): (...args: Parameters<T>) => void {
	let timeout: ReturnType<typeof setTimeout>;
	return (...args: Parameters<T>) => {
		clearTimeout(timeout);
		timeout = setTimeout(() => func(...args), wait);
	};
}

export function isNil(value: unknown): value is null | undefined {
	return value === null || value === undefined;
}

export function getProviderBrandName(host: string): string {
	if (!host) return "Unknown";

	// Check if it's an IP address
	const parts = host.toLowerCase().split(".");
	if (parts.every((part) => /^\d+$/.test(part)) && parts.length === 4) {
		return host;
	}

	const subdomainsToStrip = new Set([
		"news",
		"reader",
		"eu",
		"us",
		"ssl",
		"secure",
		"ln",
		"news-us",
		"news-eu",
		"text",
		"free",
		"ports",
		"port",
		"ipv6",
		"www",
		"upload",
	]);

	// Filter out common subdomains
	const filteredParts = parts.filter((part) => !subdomainsToStrip.has(part));

	// If we filtered out too much and have nothing left (or just 1 part), fall back to original parts
	const finalParts = filteredParts.length >= 2 ? filteredParts : parts;

	// Join back the remaining domain parts
	const brand = finalParts.join(".");

	// Capitalize the first character of the resulting domain
	return brand.charAt(0).toUpperCase() + brand.slice(1);
}
