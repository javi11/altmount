import { useState } from "react";
import { Activity, AlertTriangle, Wifi, WifiOff, ActivitySquare, ArrowUpDown, ArrowUp, ArrowDown, CheckCircle2, XCircle, Info, Gauge, RefreshCw } from "lucide-react";
import { usePoolMetrics, useProviderSpeedHistory, useTestProviderSpeed } from "../../../../hooks/useApi";
import { formatBytes, formatRelativeTime } from "../../../../lib/utils";
import { ProviderChart } from "./ProviderChart";
import { ProviderQuota } from "./ProviderQuota";
import { ProviderSpeedChart } from "./ProviderSpeedChart";
import { LineChart, Line, ResponsiveContainer, YAxis } from "recharts";
import type { ProviderSpeedTestHistoryStat, ProviderStatus } from "../../../../types/api";
import { useToast } from "../../../../contexts/ToastContext";

type SortField = 'host' | 'state' | 'used_connections' | 'missing_count' | 'current_speed_bytes_per_sec' | 'last_speed_test_mbps' | 'ping_ms' | 'error_count' | 'health_score';
type SortDirection = 'asc' | 'desc';

const SortIcon = ({ field, sortField, sortDirection }: { field: SortField, sortField: SortField, sortDirection: SortDirection }) => {
	if (sortField !== field) return <ArrowUpDown className="h-3 w-3 opacity-30" />;
	return sortDirection === 'asc' ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />;
};

const calculateHealthScore = (provider: ProviderStatus) => {
    let score = 100;
    
    // State penalty
    if (provider.state !== 'connected' && provider.state !== 'active') {
        return 0; // If disconnected, health is 0
    }
    
    // Ping penalty
    if (provider.ping_ms > 1000) score -= 40;
    else if (provider.ping_ms > 500) score -= 25;
    else if (provider.ping_ms > 200) score -= 10;
    else if (provider.ping_ms > 100) score -= 5;
    
    // Error penalty
    score -= Math.min(30, provider.error_count * 5);
    
    // Missing count penalty (warning indicator)
    if (provider.missing_warning) {
        score -= 20;
    }
    if (provider.missing_count > 5000) score -= 15;
    else if (provider.missing_count > 1000) score -= 10;
    
    return Math.max(0, score);
};

const HealthIndicator = ({ score }: { score: number }) => {
    let colorClass = "text-success";
    let icon = <CheckCircle2 className="h-4 w-4" />;
    
    if (score < 50) {
        colorClass = "text-error";
        icon = <XCircle className="h-4 w-4" />;
    } else if (score < 85) {
        colorClass = "text-warning";
        icon = <AlertTriangle className="h-4 w-4" />;
    }
    
    return (
        <div className={`flex items-center gap-1.5 font-bold ${colorClass}`}>
            {icon}
            <span>{score}%</span>
        </div>
    );
};

// Sparkline component for speed history
const SpeedHistorySparkline = ({ providerId, historyData }: { providerId: string, historyData: ProviderSpeedTestHistoryStat[] }) => {
	const providerHistory = historyData?.filter(h => h.provider_id === providerId) || [];
	// sort by created_at asc
	const sortedHistory = [...providerHistory].sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime());
	
	if (sortedHistory.length < 2) return <span className="text-base-content/50" />;

	return (
		<div className="h-8 w-20 opacity-80 transition-opacity hover:opacity-100">
			<ResponsiveContainer width="100%" height="100%">
				<LineChart data={sortedHistory}>
					<YAxis domain={['dataMin', 'dataMax']} hide />
					<Line type="stepAfter" dataKey="speed_mbps" stroke="#10b981" strokeWidth={1.5} dot={false} isAnimationActive={false} />
				</LineChart>
			</ResponsiveContainer>
		</div>
	);
};

export function ProviderHealth() {
	const { data, isLoading, error } = usePoolMetrics();
	const { data: speedHistoryResponse } = useProviderSpeedHistory(7); // Last 7 days
	const testSpeed = useTestProviderSpeed();
	const { showToast } = useToast();

	const [sortField, setSortField] = useState<SortField>('host');
	const [sortDirection, setSortDirection] = useState<SortDirection>('asc');
	const [testingId, setTestingId] = useState<string | null>(null);

	if (isLoading) {
		return (
			<div className="flex items-center justify-center p-8">
				<span className="loading loading-spinner loading-lg text-primary" />
			</div>
		);
	}

	if (error) {
		return (
			<div className="alert alert-error">
				<AlertTriangle className="h-6 w-6" />
				<span>Failed to load provider metrics: {(error as Error).message}</span>
			</div>
		);
	}

	if (!data) {
		return null;
	}

	const totalMaxConnections = data.providers.reduce(
		(sum, provider) => sum + provider.max_connections,
		0,
	);
	const totalUsedConnections = data.providers.reduce((sum, provider) => {
		if (provider.state === "connected" || provider.state === "active") {
			return sum + provider.used_connections;
		}
		return sum;
	}, 0);

	const connectionPercent = totalMaxConnections > 0 ? Math.round((totalUsedConnections / totalMaxConnections) * 100) : 0;

	const maxedProviders = data.providers.filter(p => p.quota_bytes && p.quota_bytes > 0 && p.quota_used && p.quota_used >= p.quota_bytes);
	const nearMaxProviders = data.providers.filter(p => p.quota_bytes && p.quota_bytes > 0 && p.quota_used && p.quota_used >= p.quota_bytes * 0.85 && p.quota_used < p.quota_bytes);

	const handleSort = (field: SortField) => {
		if (sortField === field) {
			setSortDirection(sortDirection === 'asc' ? 'desc' : 'asc');
		} else {
			setSortField(field);
			setSortDirection('desc'); // Default to desc for most metrics
		}
	};

	const handleRunSpeedTest = async (id: string, host: string) => {
		setTestingId(id);
		try {
			const result = await testSpeed.mutateAsync(id);
			showToast({
				type: "success",
				title: "Speed Test Completed",
				message: `${host}: ${result.speed_mbps.toFixed(2)} Mbps`,
			});
		} catch (err) {
			showToast({
				type: "error",
				title: "Speed Test Failed",
				message: (err as Error).message,
			});
		} finally {
			setTestingId(null);
		}
	};

	const sortedProviders = [...data.providers].map(p => ({ ...p, health_score: calculateHealthScore(p) })).sort((a, b) => {
		const aRaw = a[sortField as keyof typeof a];
		const bRaw = b[sortField as keyof typeof b];

		let aValue: string | number = 0;
		let bValue: string | number = 0;

		if (sortField === 'host' || sortField === 'state') {
			aValue = aRaw?.toString().toLowerCase() || '';
			bValue = bRaw?.toString().toLowerCase() || '';
		} else {
			aValue = Number(aRaw) || 0;
			bValue = Number(bRaw) || 0;
		}

		if (aValue < bValue) return sortDirection === 'asc' ? -1 : 1;
		if (aValue > bValue) return sortDirection === 'asc' ? 1 : -1;
		return 0;
	});

	return (
		<div className="space-y-6">
			{maxedProviders.length > 0 && (
				<div className="alert alert-error shadow-lg">
					<AlertTriangle className="h-6 w-6 shrink-0" />
					<div>
						<h3 className="font-bold">Quota Exceeded</h3>
						<div className="text-sm">
							{maxedProviders.length === 1 
								? `${maxedProviders[0].host} has reached its data limit. Downloads from this provider are paused.`
								: `${maxedProviders.length} providers have reached their data limits. Downloads from these providers are paused.`}
							{" "}You can reset the quota manually below.
						</div>
					</div>
				</div>
			)}
			{nearMaxProviders.length > 0 && (
				<div className="alert alert-warning shadow-lg">
					<AlertTriangle className="h-6 w-6 shrink-0" />
					<div>
						<h3 className="font-bold">Quota Warning</h3>
						<div className="text-sm">
							{nearMaxProviders.length === 1 
								? `${nearMaxProviders[0].host} is approaching its data limit.`
								: `${nearMaxProviders.length} providers are approaching their data limits.`}
						</div>
					</div>
				</div>
			)}

			{/* Global Metrics Cards */}
			<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
				<div className="stat rounded-box bg-base-100 shadow">
					<div className="stat-figure text-primary">
						<Activity className="h-8 w-8" />
					</div>
					<div className="stat-title">Download Traffic</div>
					<div className="stat-value text-2xl text-primary">
						{formatBytes(data.bytes_downloaded)}
					</div>
					<div className="stat-desc font-mono">
						{formatBytes(data.download_speed_bytes_per_sec)}/s
					</div>
				</div>

				<div className="stat rounded-box bg-base-100 shadow">
					<div className="stat-figure text-secondary">
						<ActivitySquare className="h-8 w-8" />
					</div>
					<div className="stat-title">Articles</div>
					<div className="stat-value text-2xl text-secondary">
						{data.articles_downloaded.toLocaleString()}
					</div>
					<div className="stat-desc">Downloaded</div>
				</div>

				<div className="stat rounded-box bg-base-100 shadow">
					<div className="stat-figure text-error">
						<AlertTriangle className="h-8 w-8" />
					</div>
					<div className="stat-title">Total Errors</div>
					<div className="stat-value text-2xl text-error">{data.total_errors.toLocaleString()}</div>
					<div className="stat-desc">Across all providers</div>
				</div>

				<div className="stat rounded-box bg-base-100 shadow">
					<div className="stat-figure text-info">
						<div 
							className="radial-progress border-4 border-base-200 text-info" 
							style={{"--value": connectionPercent, "--size": "3.5rem", "--thickness": "0.4rem"} as React.CSSProperties}
							role="progressbar"
						>
							<span className="font-bold text-xs">{connectionPercent}%</span>
						</div>
					</div>
					<div className="stat-title">Active Connections</div>
					<div className="stat-value text-2xl text-info">
						{totalUsedConnections}
						<span className="text-base-content/50 text-lg"> / {totalMaxConnections}</span>
					</div>
				</div>
			</div>

			{/* Data Usage & Speed History section */}
			<div className="flex flex-col gap-6">
				<ProviderChart />
				<ProviderSpeedChart />
				<ProviderQuota />
			</div>

			{/* Provider Table */}
			<div className="card bg-base-100 shadow-xl">
				<div className="card-body p-0">
					<div className="flex items-center justify-between border-base-200 border-b p-4">
						<h2 className="card-title text-lg">Provider Status</h2>
                        <div className="badge badge-outline gap-2 py-3">
                            <Info className="h-3.5 w-3.5" />
                            <span className="text-xs">Real-time stats updated every 5s</span>
                        </div>
					</div>
					<div className="overflow-x-auto">
						<table className="table-zebra table">
							<thead>
								<tr>
									<th className="cursor-pointer transition-colors hover:bg-base-200" onClick={() => handleSort('host')}>
										<div className="flex items-center gap-1">Provider Host <SortIcon sortField={sortField} sortDirection={sortDirection} field="host" /></div>
									</th>
									<th className="cursor-pointer transition-colors hover:bg-base-200" onClick={() => handleSort('health_score')}>
										<div className="flex items-center gap-1">Health <SortIcon sortField={sortField} sortDirection={sortDirection} field="health_score" /></div>
									</th>
									<th className="cursor-pointer transition-colors hover:bg-base-200" onClick={() => handleSort('state')}>
										<div className="flex items-center gap-1">State <SortIcon sortField={sortField} sortDirection={sortDirection} field="state" /></div>
									</th>
									<th className="cursor-pointer transition-colors hover:bg-base-200" onClick={() => handleSort('used_connections')}>
										<div className="flex items-center gap-1">Connections <SortIcon sortField={sortField} sortDirection={sortDirection} field="used_connections" /></div>
									</th>
									<th className="cursor-pointer transition-colors hover:bg-base-200" onClick={() => handleSort('ping_ms')}>
										<div className="flex items-center gap-1">Ping <SortIcon sortField={sortField} sortDirection={sortDirection} field="ping_ms" /></div>
									</th>
									<th className="cursor-pointer transition-colors hover:bg-base-200" onClick={() => handleSort('error_count')}>
										<div className="flex items-center gap-1">Errors <SortIcon sortField={sortField} sortDirection={sortDirection} field="error_count" /></div>
									</th>
									<th className="cursor-pointer transition-colors hover:bg-base-200" onClick={() => handleSort('missing_count')}>
										<div className="flex items-center gap-1">Missing <SortIcon sortField={sortField} sortDirection={sortDirection} field="missing_count" /></div>
									</th>
									<th className="cursor-pointer transition-colors hover:bg-base-200" onClick={() => handleSort('current_speed_bytes_per_sec')}>
										<div className="flex items-center gap-1">Current Speed <SortIcon sortField={sortField} sortDirection={sortDirection} field="current_speed_bytes_per_sec" /></div>
									</th>
									<th className="cursor-pointer transition-colors hover:bg-base-200" onClick={() => handleSort('last_speed_test_mbps')}>
										<div className="flex items-center gap-1">Top Speed <SortIcon sortField={sortField} sortDirection={sortDirection} field="last_speed_test_mbps" /></div>
									</th>
									<th>Actions</th>
								</tr>
							</thead>
							<tbody>
								{sortedProviders.map((provider) => (
									<tr key={provider.id}>
										<td className="font-medium">
											<div className="flex flex-col">
												<span>{provider.host}</span>
											</div>
										</td>
										<td>
                                            <HealthIndicator score={provider.health_score} />
										</td>
										<td>
											<div className="flex items-center gap-2">
												{provider.state === "connected" || provider.state === "active" ? (
													<span className="badge badge-success badge-sm gap-1">
														<Wifi className="h-3 w-3" /> Connected
													</span>
												) : provider.state === "disconnected" ? (
													<span className="badge badge-ghost badge-sm gap-1">
														<WifiOff className="h-3 w-3" /> Disconnected
													</span>
												) : (
													<span className="badge badge-warning badge-sm">{provider.state}</span>
												)}
											</div>
										</td>
										<td>
											<div className="flex items-center gap-2">
												<progress
													className="progress progress-primary w-16"
													value={provider.used_connections}
													max={provider.max_connections}
												/>
												<span className="font-mono text-sm">
													{provider.used_connections}/{provider.max_connections}
												</span>
											</div>
										</td>
										<td>
											<span className={`font-mono text-sm ${provider.ping_ms > 200 ? 'text-warning' : provider.ping_ms > 500 ? 'text-error' : ''}`}>
												{provider.ping_ms > 0 ? `${provider.ping_ms}ms` : '-'}
											</span>
										</td>
										<td>
											<span className={`font-mono text-sm ${provider.error_count > 0 ? 'text-error' : ''}`}>
												{provider.error_count}
											</span>
										</td>
										<td>
											{provider.missing_count > 0 ? (
												<div className="flex flex-col">
													<span
														className={`font-medium ${provider.missing_warning ? "text-error" : "text-warning"}`}
													>
														{provider.missing_count.toLocaleString()}
													</span>
												</div>
											) : (
												<span className="text-base-content/50">0</span>
											)}
										</td>
										<td>
											{provider.current_speed_bytes_per_sec > 0 ? (
												<span className="font-medium font-mono text-info">
													{formatBytes(provider.current_speed_bytes_per_sec)}/s
												</span>
											) : (
												<span className="text-base-content/50">-</span>
											)}
										</td>
										<td>
											{provider.last_speed_test_mbps > 0 ? (
												<div className="flex items-center gap-3">
													<div className="flex min-w-[70px] flex-col">
														<span className="font-medium text-success">
															{provider.last_speed_test_mbps.toFixed(2)} Mbps
														</span>
														{provider.last_speed_test_time && (
															<span className="text-base-content/50 text-xs">
																{formatRelativeTime(provider.last_speed_test_time)}
															</span>
														)}
													</div>
													{speedHistoryResponse?.history && (
														<SpeedHistorySparkline 
															providerId={provider.id} 
															historyData={speedHistoryResponse.history} 
														/>
													)}
												</div>
											) : (
												<span className="text-base-content/50">-</span>
											)}
										</td>
										<td>
											<div className="flex items-center gap-2">
												<button 
													className="btn btn-ghost btn-xs gap-1"
													onClick={() => handleRunSpeedTest(provider.id, provider.host)}
													disabled={testingId === provider.id}
													title="Run Speed Test"
												>
													{testingId === provider.id ? (
														<RefreshCw className="h-3.5 w-3.5 animate-spin" />
													) : (
														<Gauge className="h-3.5 w-3.5" />
													)}
													<span>Test</span>
												</button>
											</div>
										</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				</div>
			</div>
		</div>
	);
}
