import {
	AlertCircle,
	FileWarning,
	HardDrive,
	Lock,
	PackageOpen,
	TimerOff,
	Wifi,
	XCircle,
} from "lucide-react";
import { memo } from "react";
import { cn } from "../../lib/utils";

export type FailureCategory =
	| "articles_not_found"
	| "provider_error"
	| "extraction_failed"
	| "corrupted_file"
	| "timeout"
	| "cancelled"
	| "password_needed"
	| "disk_full"
	| "internal";

interface CategoryConfig {
	icon: React.ComponentType<{ className?: string; "aria-hidden"?: boolean }>;
	style: string;
	label: string;
	desc: string;
}

const CATEGORY_CONFIG: Record<string, CategoryConfig> = {
	articles_not_found: {
		icon: AlertCircle,
		style: "alert-warning",
		label: "Missing Articles",
		desc: "Some segments could not be found on any provider.",
	},
	provider_error: {
		icon: Wifi,
		style: "alert-error",
		label: "Provider Error",
		desc: "The Usenet provider connection failed.",
	},
	extraction_failed: {
		icon: PackageOpen,
		style: "alert-warning",
		label: "Extraction Failed",
		desc: "Could not extract the archive contents.",
	},
	corrupted_file: {
		icon: FileWarning,
		style: "alert-error",
		label: "Corrupted File",
		desc: "File data is damaged or incomplete.",
	},
	timeout: {
		icon: TimerOff,
		style: "alert-warning",
		label: "Timeout",
		desc: "The operation took too long to complete.",
	},
	cancelled: {
		icon: XCircle,
		style: "alert-info",
		label: "Cancelled",
		desc: "Processing was cancelled by request.",
	},
	password_needed: {
		icon: Lock,
		style: "alert-info",
		label: "Password Required",
		desc: "This file is password protected.",
	},
	disk_full: {
		icon: HardDrive,
		style: "alert-error",
		label: "Disk Full",
		desc: "No disk space remaining for output.",
	},
	internal: {
		icon: AlertCircle,
		style: "alert-error",
		label: "Internal Error",
		desc: "An unexpected error occurred.",
	},
};

interface CategorizedErrorAlertProps {
	category: string;
	errorMessage?: string;
	className?: string;
}

export const CategorizedErrorAlert = memo(function CategorizedErrorAlert({
	category,
	errorMessage,
	className,
}: CategorizedErrorAlertProps) {
	const config = CATEGORY_CONFIG[category] ?? CATEGORY_CONFIG.internal;
	const Icon = config.icon;

	return (
		<div className={cn("alert", config.style, "px-3 py-2", className)}>
			<Icon className="h-4 w-4 shrink-0" aria-hidden={true} />
			<div className="min-w-0 flex-1">
				<div className="font-semibold text-sm">{config.label}</div>
				{errorMessage && <div className="text-xs opacity-80">{errorMessage}</div>}
				<details className="mt-1">
					<summary className="cursor-pointer text-[11px] opacity-60 hover:opacity-100">
						Technical details
					</summary>
					<div className="mt-1 break-all rounded bg-black/10 p-2 font-mono text-[11px]">
						{config.desc}
						{category !== "internal" && (
							<span className="ml-1 opacity-50">Category: {category}</span>
						)}
					</div>
				</details>
			</div>
		</div>
	);
});
