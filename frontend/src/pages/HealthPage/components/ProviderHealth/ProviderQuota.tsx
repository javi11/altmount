import { HardDrive, RefreshCw } from "lucide-react";
import { useState } from "react";
import { usePoolMetrics, useResetProviderQuota } from "../../../../hooks/useApi";
import { formatBytes, formatRelativeTime, getProviderBrandName } from "../../../../lib/utils";
import type { ProviderStatus } from "../../../../types/api";

export function ProviderQuota() {
	const { data, isLoading } = usePoolMetrics();
	const resetQuotaMutation = useResetProviderQuota();
	const [resettingId, setResettingId] = useState<string | null>(null);

	if (isLoading || !data) return null;

	const providersWithQuota = data.providers.filter(
		(p: ProviderStatus) => p.quota_bytes && p.quota_bytes > 0,
	);

	if (providersWithQuota.length === 0) {
		return null; // Don't show the section if no providers have quotas
	}

	const handleReset = async (providerId: string) => {
		if (window.confirm("Are you sure you want to reset the quota for this provider?")) {
			setResettingId(providerId);
			try {
				await resetQuotaMutation.mutateAsync(providerId);
			} finally {
				setResettingId(null);
			}
		}
	};

	return (
		<div className="card overflow-hidden border border-base-200/40 bg-base-100/20 shadow-2xl backdrop-blur-md">
			<div className="card-body p-4 sm:p-6">
				<div className="mb-6 flex items-center justify-between border-base-200/50 border-b pb-4">
					<div>
						<h2 className="flex items-center gap-2 font-bold text-base text-base-content/90">
							<HardDrive className="h-5 w-5 animate-pulse text-primary" />
							Liquid Quota Canisters
						</h2>
						<p className="mt-0.5 text-base-content/50 text-xs">
							Tactile glass fluid indicators monitoring remaining provider capacities
						</p>
					</div>
				</div>

				<div className="grid grid-cols-1 gap-6 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
					{providersWithQuota.map((provider: ProviderStatus) => {
						const used = provider.quota_used || 0;
						const total = provider.quota_bytes || 0;
						const percentage = total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0;
						const remainingPercent = 100 - percentage;

						const isWarning = percentage >= 80 && percentage < 95;
						const isError = percentage >= 95;

						return (
							<div
								key={provider.id}
								className="group relative flex flex-col items-center justify-between rounded-xl border border-base-200/30 bg-black/25 p-4 shadow-lg transition-all duration-300 hover:border-primary/20"
							>
								{/* Card Header with Host and Reset button */}
								<div className="mb-4 flex w-full items-center justify-between border-base-200/30 border-b pb-2">
									<span
										className="max-w-[130px] truncate font-bold text-slate-100 text-sm tracking-wide"
										title={provider.host}
									>
										{getProviderBrandName(provider.host)}
									</span>
									{percentage > 0 && (
										<button
											type="button"
											className="btn btn-xs btn-ghost btn-circle text-slate-400 hover:text-white"
											onClick={() => handleReset(provider.id)}
											disabled={resettingId === provider.id}
											title="Reset Quota"
										>
											<RefreshCw
												className={`h-3.5 w-3.5 ${
													resettingId === provider.id ? "animate-spin text-primary" : ""
												}`}
											/>
										</button>
									)}
								</div>

								{/* Physical Cylinder/Vial with Tick Marks */}
								<div className="relative flex h-[150px] w-full items-center justify-center py-4">
									{/* Left Tick Marks */}
									<div className="pointer-events-none absolute left-[15%] flex h-[120px] select-none flex-col justify-between pr-2 text-right font-mono text-[8px] text-slate-500">
										<span>100%</span>
										<span>75%</span>
										<span>50%</span>
										<span>25%</span>
										<span>0%</span>
									</div>

									{/* The Glass Canister */}
									<div className="vial-container scale-105 shadow-inner">
										{/* Glow Backdrop inside */}
										<div className="pointer-events-none absolute inset-0 bg-radial-gradient from-transparent to-black/50" />

										{/* Liquid Cylinder */}
										<div
											className={`vial-liquid ${isError ? "error" : isWarning ? "warning" : ""}`}
											style={{ height: `${remainingPercent}%` }}
										>
											{/* Bouncy Liquid wave masking */}
											<div className="vial-wave" />
											<div
												className="vial-wave opacity-40"
												style={{
													animationDelay: "-3s",
													animationDuration: "5s",
												}}
											/>
										</div>

										{/* Realistic Glass Shine & Highlights */}
										<div className="pointer-events-none absolute top-0 bottom-0 left-[3px] w-[5px] rounded-full bg-gradient-to-r from-white/20 to-transparent" />
										<div className="pointer-events-none absolute top-0 right-[3px] bottom-0 w-[2px] rounded-full bg-white/5" />
										<div className="pointer-events-none absolute top-[8px] right-[6px] left-[6px] h-[5px] rounded-full bg-white/10 blur-[1px]" />
									</div>

									{/* Numeric Percentage display rotated */}
									<div className="pointer-events-none absolute right-[15%] select-none pl-2 font-bold font-mono text-[9px] text-slate-400">
										<div className="flex flex-col items-center">
											<span
												className={
													isError ? "text-error" : isWarning ? "text-warning" : "text-emerald-400"
												}
											>
												{remainingPercent}%
											</span>
											<span className="mt-0.5 text-[7px] text-slate-500 uppercase tracking-wider">
												Left
											</span>
										</div>
									</div>
								</div>

								{/* Numerical stats at the bottom */}
								<div className="mt-4 w-full space-y-1 text-center">
									<div className="font-mono font-semibold text-slate-300 text-xs">
										{formatBytes(used)} / {formatBytes(total)}
									</div>
									<div className="font-mono text-[10px] text-base-content/40">
										{percentage}% consumed
									</div>

									<div className="pt-2">
										{provider.quota_reset_at ? (
											<div className="inline-block rounded border border-primary/10 bg-primary/5 px-1 py-1 font-mono text-[9px] text-primary">
												Resets {formatRelativeTime(provider.quota_reset_at)}
											</div>
										) : (
											<div className="py-1 font-mono text-[9px] text-slate-500">
												No schedule reset
											</div>
										)}
									</div>
								</div>
							</div>
						);
					})}
				</div>
			</div>
		</div>
	);
}
