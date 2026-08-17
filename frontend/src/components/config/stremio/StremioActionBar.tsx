import { Activity, Check, Copy, ExternalLink, QrCode, Tv, Zap } from "lucide-react";
import { useEffect, useState } from "react";
import { copyToClipboard } from "../../../lib/utils";
import { QrCodeModal } from "./QrCodeModal";

interface StremioActionBarProps {
	enabled: boolean;
	baseUrl: string;
	downloadKey?: string;
	onToggleEnabled: (enabled: boolean) => void;
	isReadOnly?: boolean;
}

export function StremioActionBar({
	enabled,
	baseUrl,
	downloadKey,
	onToggleEnabled,
	isReadOnly = false,
}: StremioActionBarProps) {
	const [copied, setCopied] = useState(false);
	const [showQrModal, setShowQrModal] = useState(false);
	const [latencyMs, setLatencyMs] = useState<number | null>(null);
	const [latencyStatus, setLatencyStatus] = useState<"idle" | "testing" | "ok" | "error">("idle");

	// Compute manifest and deep link URLs
	const origin = typeof window !== "undefined" ? window.location.origin : "";
	const effectiveBase = (baseUrl || origin).replace(/\/$/, "");
	const manifestPath = downloadKey
		? `/stremio/${downloadKey}/manifest.json`
		: "/stremio/manifest.json";
	const fullManifestUrl = `${effectiveBase}${manifestPath}`;
	const stremioDeepLink = `stremio://${fullManifestUrl.replace(/^https?:\/\//, "")}`;

	// Live latency check
	useEffect(() => {
		if (!enabled) {
			setLatencyMs(null);
			setLatencyStatus("idle");
			return;
		}

		let isMounted = true;
		const pingManifest = async () => {
			setLatencyStatus("testing");
			const start = performance.now();
			try {
				const response = await fetch(manifestPath, {
					method: "GET",
					cache: "no-store",
				});
				const end = performance.now();
				if (!isMounted) return;
				if (response.ok) {
					setLatencyMs(Math.round(end - start));
					setLatencyStatus("ok");
				} else {
					setLatencyStatus("error");
				}
			} catch {
				if (!isMounted) return;
				setLatencyStatus("error");
			}
		};

		pingManifest();
		const interval = setInterval(pingManifest, 30000); // Ping every 30s

		return () => {
			isMounted = false;
			clearInterval(interval);
		};
	}, [enabled, manifestPath]);

	const handleCopy = async () => {
		const ok = await copyToClipboard(fullManifestUrl);
		if (ok) {
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		}
	};

	const handleInstall = () => {
		if (!enabled) return;
		window.open(stremioDeepLink, "_blank");
	};

	return (
		<>
			<div className="min-w-0 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 shadow-sm">
				{/* Section Header */}
				<div className="flex flex-wrap items-center justify-between gap-4 border-base-300/50 border-b pb-4">
					<div className="flex items-center gap-2.5">
						<div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
							<Tv className="h-4 w-4" />
						</div>
						<div>
							<h3 className="font-bold text-base tracking-tight">Addon Endpoint & Quick Actions</h3>
							<p className="text-base-content/60 text-xs">
								Connect your Stremio client to the Altmount usenet streaming engine
							</p>
						</div>
					</div>

					<div className="flex items-center gap-3">
						<span className="text-base-content/70 text-xs">Enable Integration</span>
						<input
							type="checkbox"
							className="toggle toggle-primary toggle-sm"
							checked={enabled}
							disabled={isReadOnly}
							onChange={(e) => onToggleEnabled(e.target.checked)}
						/>
					</div>
				</div>

				{/* Status & Latency Bar */}
				<div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-base-300 bg-base-100/70 px-4 py-2.5">
					<div className="flex items-center gap-2">
						<span className="text-base-content/60 text-xs">Status:</span>
						{enabled ? (
							<div className="flex items-center gap-1.5 font-semibold text-success text-xs">
								<span className="relative flex h-2 w-2">
									<span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-75" />
									<span className="relative inline-flex h-2 w-2 rounded-full bg-success" />
								</span>
								<span>ACTIVE (Serving Streams)</span>
							</div>
						) : (
							<div className="flex items-center gap-1.5 font-semibold text-base-content/50 text-xs">
								<span className="h-2 w-2 rounded-full bg-base-content/30" />
								<span>INACTIVE (Integration Disabled)</span>
							</div>
						)}
					</div>

					<div className="flex items-center gap-2">
						<Activity className="h-3.5 w-3.5 text-base-content/50" />
						<span className="text-base-content/60 text-xs">Endpoint Latency:</span>
						{enabled ? (
							latencyStatus === "testing" && latencyMs === null ? (
								<span className="text-base-content/50 text-xs">Testing...</span>
							) : latencyStatus === "ok" && latencyMs !== null ? (
								<span
									className={`badge badge-sm font-medium font-mono ${
										latencyMs < 50
											? "badge-success"
											: latencyMs < 150
												? "badge-warning"
												: "badge-error"
									}`}
								>
									{latencyMs}ms
								</span>
							) : (
								<span className="badge badge-sm badge-ghost text-xs">Standby</span>
							)
						) : (
							<span className="text-base-content/40 text-xs">Off</span>
						)}
					</div>
				</div>

				{/* Manifest URL Input & Actions */}
				<div className="mt-5 space-y-3">
					<div className="font-semibold text-base-content/70 text-xs">Manifest URL</div>
					<div className="flex min-w-0 flex-col gap-2 sm:flex-row">
						<div className="relative min-w-0 flex-1">
							<input
								type="text"
								readOnly
								value={enabled ? fullManifestUrl : "Enable Stremio integration above to activate"}
								className="input input-bordered w-full min-w-0 font-mono text-base-content/90 text-xs"
							/>
						</div>
						<div className="flex shrink-0 items-center gap-2">
							<button
								type="button"
								className="btn btn-outline btn-sm gap-1.5"
								onClick={handleCopy}
								disabled={!enabled}
								title="Copy Manifest Link"
							>
								{copied ? (
									<>
										<Check className="h-3.5 w-3.5 text-success" />
										<span className="text-success text-xs">Copied</span>
									</>
								) : (
									<>
										<Copy className="h-3.5 w-3.5" />
										<span className="text-xs">Copy Link</span>
									</>
								)}
							</button>
							<button
								type="button"
								className="btn btn-outline btn-sm gap-1.5"
								onClick={() => setShowQrModal(true)}
								disabled={!enabled}
								title="Open QR Code for Smart TV"
							>
								<QrCode className="h-3.5 w-3.5 text-primary" />
								<span className="text-xs">QR Code</span>
							</button>
						</div>
					</div>

					{/* Deep-Link Install Button */}
					<button
						type="button"
						className="btn btn-primary btn-block gap-2 shadow-lg shadow-primary/20"
						onClick={handleInstall}
						disabled={!enabled}
					>
						<Zap className="h-4 w-4" />
						<span>Install to Stremio (Deep-Link stremio://)</span>
						<ExternalLink className="h-3.5 w-3.5 opacity-70" />
					</button>
				</div>
			</div>

			{/* QR Code Setup Modal */}
			<QrCodeModal
				isOpen={showQrModal}
				onClose={() => setShowQrModal(false)}
				manifestUrl={fullManifestUrl}
			/>
		</>
	);
}
