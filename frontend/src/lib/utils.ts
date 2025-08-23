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

	return `${parseFloat((bytes / k ** i).toFixed(dm))} ${sizes[i]}`;
}

export function formatDuration(seconds: number) {
	const units = [
		{ label: "d", seconds: 86400 },
		{ label: "h", seconds: 3600 },
		{ label: "m", seconds: 60 },
		{ label: "s", seconds: 1 },
	];

	const parts: string[] = [];

	for (const unit of units) {
		const count = Math.floor(seconds / unit.seconds);
		if (count > 0) {
			parts.push(`${count}${unit.label}`);
			seconds -= count * unit.seconds;
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
	if (diffInSeconds < 2592000)
		return `${Math.floor(diffInSeconds / 86400)}d ago`;

	return target.toLocaleDateString();
}

export function getStatusColor(status: string): string {
	switch (status.toLowerCase()) {
		case "healthy":
		case "completed":
			return "success";
		case "processing":
		case "retrying":
			return "info";
		case "pending":
			return "warning";
		case "failed":
		case "corrupted":
		case "unhealthy":
			return "error";
		case "partial":
		case "degraded":
			return "warning";
		default:
			return "neutral";
	}
}

export function truncateText(text: string, maxLength: number = 50): string {
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