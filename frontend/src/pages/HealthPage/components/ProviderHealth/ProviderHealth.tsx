import {
	Activity,
	ActivitySquare,
	AlertTriangle,
	ArrowDown,
	ArrowUp,
	ArrowUpDown,
	CheckCircle2,
	Gauge,
	Info,
	RefreshCw,
	Wifi,
	WifiOff,
	XCircle,
} from "lucide-react";
import { useState } from "react";
import { Line, LineChart, ResponsiveContainer, YAxis } from "recharts";
import { useToast } from "../../../../contexts/ToastContext";
import {
	usePoolMetrics,
	useProviderSpeedHistory,
	useTestProviderSpeed,
} from "../../../../hooks/useApi";
import { formatBytes, formatRelativeTime, getProviderBrandName } from "../../../../lib/utils";
import type { ProviderSpeedTestHistoryStat, ProviderStatus } from "../../../../types/api";
import { ProviderChart } from "./ProviderChart";
import { ProviderQuota } from "./ProviderQuota";
import { ProviderSpeedChart } from "./ProviderSpeedChart";

type SortField =
	| "host"
	| "state"
	| "used_connections"
	| "missing_count"
	| "current_speed_bytes_per_sec"
	| "last_speed_test_mbps"
	| "ping_ms"
	| "error_count"
	| "health_score";
type SortDirection = "asc" | "desc";

const SortIcon = ({
	field,
	sortField,
	sortDirection,
}: {
	field: SortField;
	sortField: SortField;
	sortDirection: SortDirection;
}) => {
	if (sortField !== field) return <ArrowUpDown className="h-3 w-3 opacity-30" />;
	return sortDirection === "asc" ? (
		<ArrowUp className="h-3 w-3" />
	) : (
		<ArrowDown className="h-3 w-3" />
	);
};

const calculateHealthScore = (provider: ProviderStatus) => {
	let score = 100;

	// State penalty
	if (provider.state !== "connected" && provider.state !== "active") {
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
const SpeedHistorySparkline = ({
	providerId,
	historyData,
}: {
	providerId: string;
	historyData: ProviderSpeedTestHistoryStat[];
}) => {
	const providerHistory = historyData?.filter((h) => h.provider_id === providerId) || [];
	// sort by created_at asc
	const sortedHistory = [...providerHistory].sort(
		(a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
	);

	if (sortedHistory.length < 2) return <span className="text-base-content/50" />;

	return (
		<div className="h-8 w-20 opacity-80 transition-opacity hover:opacity-100">
			<ResponsiveContainer width="100%" height="100%">
				<LineChart data={sortedHistory}>
					<YAxis domain={["dataMin", "dataMax"]} hide />
					<Line
						type="stepAfter"
						dataKey="speed_mbps"
						stroke="#10b981"
						strokeWidth={1.5}
						dot={false}
						isAnimationActive={false}
					/>
				</LineChart>
			</ResponsiveContainer>
		</div>
	);
};

function ConnectionPoolGrid({ used, max }: { used: number; max: number }) {
	if (max > 20) {
		const percent = Math.round((used / max) * 100);
		return (
			<div className="flex items-center gap-2">
				<div className="flex h-2.5 w-16 overflow-hidden rounded-full border border-base-content/10 bg-base-200/50">
					<div
						className="h-full rounded-full bg-primary shadow-[0_0_8px_rgba(59,130,246,0.5)] transition-all duration-500"
						style={{ width: `${percent}%` }}
					/>
				</div>
				<span className="font-mono text-base-content/80 text-xs">
					{used}/{max}
				</span>
			</div>
		);
	}

	return (
		<div className="flex items-center gap-2">
			<div className="flex max-w-[80px] flex-wrap gap-0.5">
				{Array.from({ length: max }).map((_, i) => (
					<span
						key={i}
						className={`h-3 w-1 rounded-sm transition-all duration-300 ${
							i < used
								? "bg-primary shadow-[0_0_6px_rgba(59,130,246,0.6)]"
								: "border border-base-200 bg-base-200/50"
						}`}
					/>
				))}
			</div>
			<span className="font-mono text-base-content/85 text-xs">
				{used}/{max}
			</span>
		</div>
	);
}

export function ProviderHealth() {
	const { data, isLoading, error } = usePoolMetrics();
	const { data: speedHistoryResponse } = useProviderSpeedHistory(7); // Last 7 days
	const testSpeed = useTestProviderSpeed();
	const { showToast } = useToast();

	const [sortField, setSortField] = useState<SortField>("host");
	const [sortDirection, setSortDirection] = useState<SortDirection>("asc");
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

	const connectionPercent =
		totalMaxConnections > 0 ? Math.round((totalUsedConnections / totalMaxConnections) * 100) : 0;

	const maxedProviders = data.providers.filter(
		(p) => p.quota_bytes && p.quota_bytes > 0 && p.quota_used && p.quota_used >= p.quota_bytes,
	);
	const nearMaxProviders = data.providers.filter(
		(p) =>
			p.quota_bytes &&
			p.quota_bytes > 0 &&
			p.quota_used &&
			p.quota_used >= p.quota_bytes * 0.85 &&
			p.quota_used < p.quota_bytes,
	);

	const handleSort = (field: SortField) => {
		if (sortField === field) {
			setSortDirection(sortDirection === "asc" ? "desc" : "asc");
		} else {
			setSortField(field);
			setSortDirection("desc"); // Default to desc for most metrics
		}
	};

	const handleRunSpeedTest = async (id: string, host: string) => {
		setTestingId(id);
		try {
			const result = await testSpeed.mutateAsync(id);
			showToast({
				type: "success",
				title: "Speed Test Completed",
				message: `${host}: ${result.speed_mbps.toFixed(2)} MB/s`,
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

	const sortedProviders = [...data.providers]
		.map((p) => ({ ...p, health_score: calculateHealthScore(p) }))
		.sort((a, b) => {
			const aRaw = a[sortField as keyof typeof a];
			const bRaw = b[sortField as keyof typeof b];

			let aValue: string | number = 0;
			let bValue: string | number = 0;

			if (sortField === "host" || sortField === "state") {
				aValue = aRaw?.toString().toLowerCase() || "";
				bValue = bRaw?.toString().toLowerCase() || "";
			} else {
				aValue = Number(aRaw) || 0;
				bValue = Number(bRaw) || 0;
			}

			if (aValue < bValue) return sortDirection === "asc" ? -1 : 1;
			if (aValue > bValue) return sortDirection === "asc" ? 1 : -1;
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
								: `${maxedProviders.length} providers have reached their data limits. Downloads from these providers are paused.`}{" "}
							You can reset the quota manually below.
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
				{/* Download Traffic Card */}
				<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200/40 bg-base-100/20 p-5 shadow-xl backdrop-blur-md transition-all hover:border-primary/20">
					{data.download_speed_bytes_per_sec > 0 && (
						<div className="absolute inset-0 bg-gradient-to-tr from-primary/5 via-transparent to-transparent opacity-60" />
					)}
					<div className="relative z-10 space-y-1">
						<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
							Download Traffic
						</span>
						<div className="font-extrabold font-mono text-2xl text-primary tracking-tight">
							{formatBytes(data.bytes_downloaded)}
						</div>
						<div className="font-mono text-base-content/65 text-xs">
							{formatBytes(data.download_speed_bytes_per_sec)}/s
						</div>
					</div>
					<div className="relative z-10 text-primary">
						<Activity
							className={`h-8 w-8 ${data.download_speed_bytes_per_sec > 0 ? "animate-pulse text-primary shadow-[0_0_12px_rgba(59,130,246,0.3)]" : "opacity-45"}`}
						/>
					</div>
					{/* Active wave line on bottom of the card */}
					{data.download_speed_bytes_per_sec > 0 && (
						<div className="absolute right-0 bottom-0 left-0 h-1 overflow-hidden opacity-30">
							<svg
								viewBox="0 0 100 10"
								className="h-full w-full fill-primary"
								preserveAspectRatio="none"
							>
								<title>Download Speed Wave</title>
								<path
									d="M0,5 C30,8 70,2 100,5 L100,10 L0,10 Z"
									style={{ animation: "speed-wave 2s ease-in-out infinite" }}
								/>
							</svg>
						</div>
					)}
				</div>

				{/* Articles Card */}
				<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200/40 bg-base-100/20 p-5 shadow-xl backdrop-blur-md transition-all hover:border-secondary/20">
					<div className="z-10 space-y-1">
						<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
							Articles
						</span>
						<div className="font-extrabold font-mono text-2xl text-secondary tracking-tight">
							{data.articles_downloaded.toLocaleString()}
						</div>
						<div className="text-base-content/50 text-xs">Downloaded</div>
					</div>
					<div className="z-10 text-secondary">
						<ActivitySquare className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
					</div>
				</div>

				{/* Total Errors Card */}
				<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200/40 bg-base-100/20 p-5 shadow-xl backdrop-blur-md transition-all hover:border-error/20">
					{data.total_errors > 0 && (
						<div className="absolute inset-0 bg-gradient-to-tr from-error/5 via-transparent to-transparent opacity-60" />
					)}
					<div className="z-10 space-y-1">
						<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
							Total Errors
						</span>
						<div className="font-extrabold font-mono text-2xl text-error tracking-tight">
							{data.total_errors.toLocaleString()}
						</div>
						<div className="text-base-content/50 text-xs">Across all providers</div>
					</div>
					<div className="z-10 text-error">
						<AlertTriangle
							className={`h-8 w-8 ${data.total_errors > 0 ? "animate-bounce text-error" : "opacity-45"}`}
						/>
					</div>
				</div>

				{/* Active Connections Card */}
				<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200/40 bg-base-100/20 p-5 shadow-xl backdrop-blur-md transition-all hover:border-info/20">
					<div className="z-10 space-y-1">
						<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
							Active Connections
						</span>
						<div className="font-extrabold font-mono text-2xl text-info tracking-tight">
							{totalUsedConnections}
							<span className="font-medium text-base-content/40 text-sm">
								{" "}
								/ {totalMaxConnections}
							</span>
						</div>
						<div className="text-base-content/50 text-xs">Across active pools</div>
					</div>
					<div className="z-10 text-info">
						<div
							className="radial-progress border-2 border-base-content/10 text-info shadow-[0_0_8px_rgba(6,182,212,0.15)]"
							style={
								{
									"--value": connectionPercent,
									"--size": "3rem",
									"--thickness": "0.3rem",
								} as React.CSSProperties
							}
							role="progressbar"
						>
							<span className="font-bold font-mono text-[10px]">{connectionPercent}%</span>
						</div>
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
			<div className="card overflow-hidden border border-base-200/40 bg-base-100/20 shadow-2xl backdrop-blur-md">
				<div className="card-body p-0">
					<div className="flex items-center justify-between border-base-200/50 border-b bg-base-200/30 p-4">
						<h2 className="flex items-center gap-2 font-bold text-base text-base-content/90">
							Provider Status
						</h2>
						<div className="badge badge-outline gap-2 border-base-200/60 bg-base-100/30 py-3">
							<Info className="h-3.5 w-3.5" />
							<span className="text-[11px] text-base-content/66">
								Real-time metrics updated every 5s
							</span>
						</div>
					</div>
					<div className="overflow-x-auto">
						<table className="table-zebra table border-collapse">
							<thead>
								<tr>
									<th
										className="cursor-pointer transition-colors hover:bg-base-200"
										onClick={() => handleSort("host")}
									>
										<div className="flex items-center gap-1">
											Provider Host{" "}
											<SortIcon sortField={sortField} sortDirection={sortDirection} field="host" />
										</div>
									</th>
									<th
										className="cursor-pointer transition-colors hover:bg-base-200"
										onClick={() => handleSort("health_score")}
									>
										<div className="flex items-center gap-1">
											Health{" "}
											<SortIcon
												sortField={sortField}
												sortDirection={sortDirection}
												field="health_score"
											/>
										</div>
									</th>
									<th
										className="cursor-pointer transition-colors hover:bg-base-200"
										onClick={() => handleSort("state")}
									>
										<div className="flex items-center gap-1">
											State{" "}
											<SortIcon sortField={sortField} sortDirection={sortDirection} field="state" />
										</div>
									</th>
									<th
										className="cursor-pointer transition-colors hover:bg-base-200"
										onClick={() => handleSort("used_connections")}
									>
										<div className="flex items-center gap-1">
											Connections{" "}
											<SortIcon
												sortField={sortField}
												sortDirection={sortDirection}
												field="used_connections"
											/>
										</div>
									</th>
									<th
										className="cursor-pointer transition-colors hover:bg-base-200"
										onClick={() => handleSort("ping_ms")}
									>
										<div className="flex items-center gap-1">
											Ping{" "}
											<SortIcon
												sortField={sortField}
												sortDirection={sortDirection}
												field="ping_ms"
											/>
										</div>
									</th>
									<th
										className="cursor-pointer transition-colors hover:bg-base-200"
										onClick={() => handleSort("error_count")}
									>
										<div className="flex items-center gap-1">
											Errors{" "}
											<SortIcon
												sortField={sortField}
												sortDirection={sortDirection}
												field="error_count"
											/>
										</div>
									</th>
									<th
										className="cursor-pointer transition-colors hover:bg-base-200"
										onClick={() => handleSort("missing_count")}
									>
										<div className="flex items-center gap-1">
											Missing{" "}
											<SortIcon
												sortField={sortField}
												sortDirection={sortDirection}
												field="missing_count"
											/>
										</div>
									</th>
									<th
										className="cursor-pointer transition-colors hover:bg-base-200"
										onClick={() => handleSort("current_speed_bytes_per_sec")}
									>
										<div className="flex items-center gap-1">
											Current Speed{" "}
											<SortIcon
												sortField={sortField}
												sortDirection={sortDirection}
												field="current_speed_bytes_per_sec"
											/>
										</div>
									</th>
									<th
										className="cursor-pointer transition-colors hover:bg-base-200"
										onClick={() => handleSort("last_speed_test_mbps")}
									>
										<div className="flex items-center gap-1">
											Top Speed{" "}
											<SortIcon
												sortField={sortField}
												sortDirection={sortDirection}
												field="last_speed_test_mbps"
											/>
										</div>
									</th>
									<th>Actions</th>
								</tr>
							</thead>
							<tbody>
								{sortedProviders.map((provider) => (
									<tr
										key={provider.id}
										className="border-base-200/30 border-b transition-colors hover:bg-base-content/5"
									>
										<td>
											<div className="flex flex-col">
												<span className="font-bold text-base-content text-sm tracking-wide">
													{getProviderBrandName(provider.host)}
												</span>
												<span className="mt-0.5 font-mono text-[10px] text-base-content/40">
													{provider.host}
												</span>
											</div>
										</td>
										<td>
											<HealthIndicator score={provider.health_score} />
										</td>
										<td>
											<div className="flex items-center gap-2">
												{provider.state === "connected" || provider.state === "active" ? (
													<span className="badge badge-sm gap-1 border-emerald-500/20 bg-emerald-500/10 font-bold text-emerald-400">
														<Wifi className="h-3 w-3" /> Connected
													</span>
												) : provider.state === "disconnected" ? (
													<span className="badge badge-sm gap-1 border-base-content/20 bg-base-content/5 font-bold text-base-content/60">
														<WifiOff className="h-3 w-3" /> Disconnected
													</span>
												) : (
													<span className="badge badge-sm border-amber-500/20 bg-amber-500/10 font-bold text-amber-400">
														{provider.state}
													</span>
												)}
											</div>
										</td>
										<td>
											<ConnectionPoolGrid
												used={provider.used_connections}
												max={provider.max_connections}
											/>
										</td>
										<td>
											<div className="flex items-center gap-1.5 font-medium font-mono text-xs">
												{provider.ping_ms > 0 ? (
													<>
														<span
															className={`h-1.5 w-1.5 rounded-full ${
																provider.ping_ms > 500
																	? "bg-rose-500 shadow-[0_0_6px_rgba(244,63,94,0.6)]"
																	: provider.ping_ms > 200
																		? "bg-amber-400 shadow-[0_0_6px_rgba(251,191,36,0.6)]"
																		: "bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.6)]"
															}`}
														/>
														<span
															className={
																provider.ping_ms > 500
																	? "font-bold text-error"
																	: provider.ping_ms > 200
																		? "font-bold text-warning"
																		: "text-base-content/70"
															}
														>
															{provider.ping_ms}ms
														</span>
													</>
												) : (
													<span className="text-base-content/30">-</span>
												)}
											</div>
										</td>
										<td>
											{provider.error_count > 0 ? (
												<span className="badge badge-sm border-rose-500/20 bg-rose-500/10 font-bold font-mono text-rose-400 shadow-[0_0_6px_rgba(244,63,94,0.15)]">
													{provider.error_count}
												</span>
											) : (
												<span className="font-mono text-base-content/30 text-xs">0</span>
											)}
										</td>
										<td>
											{provider.missing_count > 0 ? (
												<span
													className={`badge badge-sm font-bold font-mono shadow-[0_0_6px_rgba(251,191,36,0.15)] ${
														provider.missing_warning
															? "border-rose-500/20 bg-rose-500/10 text-rose-400"
															: "border-amber-500/20 bg-amber-500/10 text-amber-400"
													}`}
												>
													{provider.missing_count.toLocaleString()}
												</span>
											) : (
												<span className="font-mono text-base-content/30 text-xs">0</span>
											)}
										</td>
										<td>
											{provider.current_speed_bytes_per_sec > 0 ? (
												<span className="animate-pulse font-mono font-semibold text-info text-xs">
													{formatBytes(provider.current_speed_bytes_per_sec)}/s
												</span>
											) : (
												<span className="font-mono text-base-content/30 text-xs">-</span>
											)}
										</td>
										<td>
											{provider.last_speed_test_mbps > 0 ? (
												<div className="flex items-center gap-3">
													<div className="flex min-w-[70px] flex-col">
														<span className="font-bold font-mono text-success text-xs">
															{provider.last_speed_test_mbps.toFixed(2)} MB/s
														</span>
														{provider.last_speed_test_time && (
															<span className="font-mono text-[9px] text-base-content/40">
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
												<span className="font-mono text-base-content/30 text-xs">-</span>
											)}
										</td>
										<td>
											<div className="flex items-center gap-2">
												<button
													type="button"
													className="btn btn-ghost btn-sm gap-1 border border-base-200 font-semibold text-xs hover:bg-base-200/40"
													onClick={() => handleRunSpeedTest(provider.id, provider.host)}
													disabled={testingId === provider.id}
													title="Run Speed Test"
												>
													{testingId === provider.id ? (
														<RefreshCw className="h-3.5 w-3.5 animate-spin text-primary" />
													) : (
														<Gauge className="h-3.5 w-3.5 text-primary group-hover:animate-pulse" />
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
