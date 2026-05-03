import { useState } from "react";
import { usePoolMetrics, useResetProviderQuota } from "../../../../hooks/useApi";
import { formatBytes, formatRelativeTime } from "../../../../lib/utils";
import { HardDrive, RefreshCw } from "lucide-react";
import type { ProviderStatus } from "../../../../types/api";

export function ProviderQuota() {
	const { data, isLoading } = usePoolMetrics();
	const resetQuotaMutation = useResetProviderQuota();
	const [resettingId, setResettingId] = useState<string | null>(null);

	if (isLoading || !data) return null;

	const providersWithQuota = data.providers.filter((p: ProviderStatus) => p.quota_bytes && p.quota_bytes > 0);

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
		<div className="card mb-6 bg-base-100 shadow-xl">
			<div className="card-body p-4 sm:p-6">
				<div className="mb-4 flex items-center justify-between border-base-200 border-b pb-4">
					<div>
						<h2 className="card-title flex items-center gap-2 text-lg">
							<HardDrive className="h-5 w-5" />
							Data Quotas
						</h2>
						<p className="text-base-content/60 text-sm">Provider data usage vs limits</p>
					</div>
				</div>
				
				<div className="space-y-6">
					{providersWithQuota.map((provider: ProviderStatus) => {
						const used = provider.quota_used || 0;
						const total = provider.quota_bytes || 0;
						const percentage = total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0;
						
						const isWarning = percentage >= 80 && percentage < 95;
						const isError = percentage >= 95;
						
						let progressClass = "progress-primary";
						if (isError) progressClass = "progress-error";
						else if (isWarning) progressClass = "progress-warning";

						return (
							<div key={provider.id} className="flex flex-col gap-2">
								<div className="flex items-end justify-between">
									<div className="flex flex-col">
										<div className="flex items-center gap-2">
											<span className="font-medium">{provider.host}</span>
											{percentage > 0 && (
												<button
													type="button"
													className="btn btn-xs btn-ghost btn-circle"
													onClick={() => handleReset(provider.id)}
													disabled={resettingId === provider.id}
													title="Reset Quota"
												>
													<RefreshCw className={`h-3 w-3 ${resettingId === provider.id ? "animate-spin" : ""}`} />
												</button>
											)}
										</div>
									</div>
									<div className="text-right">
										<div className="font-medium">
											{formatBytes(used)} / {formatBytes(total)}
										</div>
										{provider.quota_reset_at && (
											<div className="text-base-content/60 text-xs">
												Resets {formatRelativeTime(provider.quota_reset_at)}
											</div>
										)}
									</div>
								</div>
								<div className="flex items-center gap-3">
									<progress 
										className={`progress h-3 w-full ${progressClass}`} 
										value={percentage} 
										max="100"
									/>
									<span className={`w-10 text-right font-mono text-sm ${isError ? 'font-bold text-error' : isWarning ? 'font-bold text-warning' : ''}`}>
										{percentage}%
									</span>
								</div>
							</div>
						);
					})}
				</div>
			</div>
		</div>
	);
}
