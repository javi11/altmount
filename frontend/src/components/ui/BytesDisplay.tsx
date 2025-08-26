import { Info } from "lucide-react";

interface BytesDisplayProps {
	bytes: number;
	mode?: "inline" | "badge" | "tooltip";
}

// Utility function to format bytes to human-readable format
function formatBytes(bytes: number, decimals = 2): string {
	if (bytes === 0) return "0 B";

	const k = 1024;
	const dm = decimals < 0 ? 0 : decimals;
	const sizes = ["B", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"];

	const i = Math.floor(Math.log(bytes) / Math.log(k));

	return `${Number.parseFloat((bytes / k ** i).toFixed(dm))} ${sizes[i]}`;
}

// Utility function to format large numbers with commas
function formatNumber(num: number): string {
	return num.toLocaleString();
}

export function BytesDisplay({ bytes, mode = "inline" }: BytesDisplayProps) {
	const humanReadable = formatBytes(bytes);
	const formattedBytes = formatNumber(bytes);

	switch (mode) {
		case "badge":
			return (
				<div className="tooltip tooltip-top" data-tip={`${formattedBytes} bytes`}>
					<span className="badge badge-ghost badge-sm">{humanReadable}</span>
				</div>
			);

		case "tooltip":
			return (
				<div className="tooltip tooltip-top" data-tip={`${formattedBytes} bytes`}>
					<div className="flex items-center gap-1 text-base-content/70 text-sm">
						<Info className="h-3 w-3" />
						<span>{humanReadable}</span>
					</div>
				</div>
			);

		default:
			return (
				<span className="text-base-content/70 text-sm">
					{humanReadable} ({formattedBytes} bytes)
				</span>
			);
	}
}

// Export the utility functions for use in other components
export { formatBytes, formatNumber };
