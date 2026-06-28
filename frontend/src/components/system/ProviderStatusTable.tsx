import {
	ArrowDown,
	ArrowUp,
	ArrowUpDown,
	Gauge,
	Info,
	Network,
	RefreshCw,
	RotateCcw,
	Wifi,
	WifiOff,
} from "lucide-react";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import { useToast } from "../../contexts/ToastContext";
import { useTestProviderSpeed } from "../../hooks/useApi";
import { useConfig } from "../../hooks/useConfig";
import { useProviders } from "../../hooks/useProviders";
import {
	formatBytes,
	formatExpirationDate,
	formatRelativeTime,
	getProviderBrandName,
} from "../../lib/utils";
import type { ProviderStatus } from "../../types/api";

type SortField =
	| "host"
	| "state"
	| "used_connections"
	| "current_speed_bytes_per_sec"
	| "last_speed_test_mbps"
	| "ping_ms"
	| "error_count"
	| "byte_count"
	| "expiration";
type SortDirection = "asc" | "desc";

const SORT_FIELDS: SortField[] = [
	"host",
	"state",
	"used_connections",
	"current_speed_bytes_per_sec",
	"last_speed_test_mbps",
	"ping_ms",
	"error_count",
	"byte_count",
	"expiration",
];

const SORT_STORAGE_KEY = "altmount.providerStatus.sort";

function getProviderDisplayName(provider: ProviderStatus): string {
	const nickname = provider.name?.trim();
	return nickname && nickname.length > 0 ? nickname : getProviderBrandName(provider.host);
}

function loadSortPref(): { field: SortField; direction: SortDirection } {
	try {
		const raw = localStorage.getItem(SORT_STORAGE_KEY);
		if (raw) {
			const p = JSON.parse(raw) as { field?: unknown; direction?: unknown };
			const field: SortField =
				typeof p.field === "string" && (SORT_FIELDS as string[]).includes(p.field)
					? (p.field as SortField)
					: "host";
			const direction: SortDirection = p.direction === "desc" ? "desc" : "asc";
			return { field, direction };
		}
	} catch {
		// ignore malformed/unavailable storage
	}
	return { field: "host", direction: "asc" };
}

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

function ConnectionPoolGrid({ used, max }: { used: number; max: number }) {
	const percent = max > 0 ? Math.round((used / max) * 100) : 0;
	return (
		<div className="flex items-center gap-2">
			<div className="flex h-2.5 w-16 overflow-hidden rounded-full border border-base-content/10 bg-base-200/50">
				<div
					className="h-full rounded-full bg-primary transition-all duration-500"
					style={{ width: `${percent}%` }}
				/>
			</div>
			<span className="font-mono text-base-content/80 text-xs">
				{used}/{max}
			</span>
		</div>
	);
}

interface ProviderStatusTableProps {
	providers: ProviderStatus[];
	title?: string;
	children?: ReactNode;
}

export function ProviderStatusTable({
	providers,
	title = "Provider Status",
	children,
}: ProviderStatusTableProps) {
	const { data: configData } = useConfig();
	const testSpeed = useTestProviderSpeed();
	const { resetProviderQuota } = useProviders();
	const { showToast } = useToast();

	// Expiration dates come from config rather than runtime pool metrics.
	const expirationByKey = useMemo(() => {
		const map = new Map<string, string>();
		for (const p of configData?.providers ?? []) {
			if (!p.account_expiration_date) continue;
			map.set(p.id, p.account_expiration_date);
			map.set(p.host, p.account_expiration_date);
		}
		return map;
	}, [configData?.providers]);

	const [sortField, setSortField] = useState<SortField>(() => loadSortPref().field);
	const [sortDirection, setSortDirection] = useState<SortDirection>(() => loadSortPref().direction);
	const [testingId, setTestingId] = useState<string | null>(null);
	const [resettingId, setResettingId] = useState<string | null>(null);

	useEffect(() => {
		try {
			localStorage.setItem(
				SORT_STORAGE_KEY,
				JSON.stringify({ field: sortField, direction: sortDirection }),
			);
		} catch {
			// ignore storage write failures
		}
	}, [sortField, sortDirection]);

	const handleSort = (field: SortField) => {
		if (sortField === field) {
			setSortDirection(sortDirection === "asc" ? "desc" : "asc");
		} else {
			setSortField(field);
			setSortDirection("desc");
		}
	};

	const handleRunSpeedTest = async (id: string, label: string) => {
		setTestingId(id);
		try {
			const result = await testSpeed.mutateAsync(id);
			showToast({
				type: "success",
				title: "Speed Test Completed",
				message: `${label}: ${result.speed_mbps.toFixed(2)} MB/s`,
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

	const handleResetQuota = async (id: string, label: string) => {
		setResettingId(id);
		try {
			await resetProviderQuota.mutateAsync(id);
			showToast({
				type: "success",
				title: "Quota Reset",
				message: `${label}: quota has been reset`,
			});
		} catch (err) {
			showToast({
				type: "error",
				title: "Reset Failed",
				message: (err as Error).message,
			});
		} finally {
			setResettingId(null);
		}
	};

	const sortedProviders = [...providers].sort((a, b) => {
		// Missing expiration dates sort last.
		if (sortField === "expiration") {
			const aExp = expirationByKey.get(a.id) ?? expirationByKey.get(a.host) ?? "";
			const bExp = expirationByKey.get(b.id) ?? expirationByKey.get(b.host) ?? "";
			if (!aExp && !bExp) return 0;
			if (!aExp) return 1;
			if (!bExp) return -1;
			return sortDirection === "asc" ? aExp.localeCompare(bExp) : bExp.localeCompare(aExp);
		}

		if (sortField === "host") {
			const aValue = getProviderDisplayName(a).toLowerCase();
			const bValue = getProviderDisplayName(b).toLowerCase();
			if (aValue < bValue) return sortDirection === "asc" ? -1 : 1;
			if (aValue > bValue) return sortDirection === "asc" ? 1 : -1;
			return 0;
		}

		const aRaw = a[sortField as keyof typeof a];
		const bRaw = b[sortField as keyof typeof b];

		let aValue: string | number = 0;
		let bValue: string | number = 0;

		if (sortField === "state") {
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
		<div className="card overflow-hidden border border-base-200/40 bg-base-100/20 shadow-2xl backdrop-blur-md">
			<div className="card-body p-0">
				<div className="flex items-center justify-between border-base-200/50 border-b bg-base-200/30 p-4">
					<h2 className="flex items-center gap-2 font-semibold text-xl">
						<Network className="h-6 w-6" aria-hidden="true" />
						{title}
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
										Provider{" "}
										<SortIcon sortField={sortField} sortDirection={sortDirection} field="host" />
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
										<SortIcon sortField={sortField} sortDirection={sortDirection} field="ping_ms" />
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
								<th
									className="cursor-pointer transition-colors hover:bg-base-200"
									onClick={() => handleSort("byte_count")}
								>
									<div className="flex items-center gap-1">
										Data Usage{" "}
										<SortIcon
											sortField={sortField}
											sortDirection={sortDirection}
											field="byte_count"
										/>
									</div>
								</th>
								<th
									className="cursor-pointer transition-colors hover:bg-base-200"
									onClick={() => handleSort("expiration")}
								>
									<div className="flex items-center gap-1">
										Expiration Date{" "}
										<SortIcon
											sortField={sortField}
											sortDirection={sortDirection}
											field="expiration"
										/>
									</div>
								</th>
								<th>Actions</th>
							</tr>
						</thead>
						<tbody>
							{sortedProviders.map((provider) => {
								const hasQuota = provider.quota_bytes != null && provider.quota_bytes > 0;
								return (
									<tr
										key={provider.id}
										className="border-base-200/30 border-b transition-colors hover:bg-base-content/5"
									>
										<td>
											<div className="flex flex-col">
												<span className="font-bold text-base-content text-sm tracking-wide">
													{getProviderDisplayName(provider)}
												</span>
												<span className="mt-0.5 font-mono text-[10px] text-base-content/40">
													{provider.host}
												</span>
											</div>
										</td>
										<td>
											<div className="flex items-center gap-2">
												{provider.state === "connected" || provider.state === "active" ? (
													<span className="badge badge-sm gap-1 border-success/20 bg-success/10 font-bold text-success">
														<Wifi className="h-3 w-3" /> Connected
													</span>
												) : provider.state === "disconnected" ? (
													<span className="badge badge-sm gap-1 border-base-content/20 bg-base-content/5 font-bold text-base-content/60">
														<WifiOff className="h-3 w-3" /> Disconnected
													</span>
												) : (
													<span className="badge badge-sm border-warning/20 bg-warning/10 font-bold text-warning">
														{provider.state}
													</span>
												)}
												{provider.quota_exceeded && (
													<span className="badge badge-sm border-error/20 bg-error/10 font-bold text-error">
														Quota
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
																	? "bg-error"
																	: provider.ping_ms > 200
																		? "bg-warning"
																		: "bg-success"
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
												<span className="badge badge-sm border-error/20 bg-error/10 font-bold font-mono text-error">
													{provider.error_count}
												</span>
											) : (
												<span className="font-mono text-base-content/30 text-xs">0</span>
											)}
										</td>
										<td>
											{provider.current_speed_bytes_per_sec > 0 ? (
												<span className="font-mono font-semibold text-info text-xs">
													{formatBytes(provider.current_speed_bytes_per_sec)}/s
												</span>
											) : (
												<span className="font-mono text-base-content/30 text-xs">-</span>
											)}
										</td>
										<td>
											{provider.last_speed_test_mbps > 0 ? (
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
											) : (
												<span className="min-w-[70px] font-mono text-base-content/30 text-xs">
													-
												</span>
											)}
										</td>
										<td>
											{provider.byte_count > 0 ? (
												<div className="flex min-w-[80px] flex-col">
													<span className="font-bold font-mono text-base-content/80 text-xs">
														{formatBytes(provider.byte_count)}
													</span>
													<span className="font-mono text-[9px] text-base-content/40">
														{formatBytes(provider.byte_count_24h)} / 24h
													</span>
												</div>
											) : (
												<span className="min-w-[80px] font-mono text-base-content/30 text-xs">
													-
												</span>
											)}
										</td>
										<td>
											{(() => {
												const expiration =
													expirationByKey.get(provider.id) ?? expirationByKey.get(provider.host);
												return expiration ? (
													<span className="font-mono text-[11px] text-base-content/70">
														{formatExpirationDate(expiration)}
													</span>
												) : (
													<span className="font-mono text-base-content/30 text-xs">-</span>
												);
											})()}
										</td>
										<td>
											<div className="flex items-center gap-2">
												<button
													type="button"
													className="btn btn-ghost btn-sm gap-1 border border-base-200 font-semibold text-xs hover:bg-base-200/40"
													onClick={() =>
														handleRunSpeedTest(provider.id, getProviderDisplayName(provider))
													}
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
												{hasQuota && (
													<button
														type="button"
														className="btn btn-ghost btn-sm gap-1 border border-base-200 font-semibold text-xs hover:bg-base-200/40"
														onClick={() =>
															handleResetQuota(provider.id, getProviderDisplayName(provider))
														}
														disabled={resettingId === provider.id}
														title="Reset Quota"
													>
														{resettingId === provider.id ? (
															<RefreshCw className="h-3.5 w-3.5 animate-spin text-primary" />
														) : (
															<RotateCcw className="h-3.5 w-3.5 text-base-content/60" />
														)}
														<span>Quota</span>
													</button>
												)}
											</div>
										</td>
									</tr>
								);
							})}
						</tbody>
					</table>
				</div>
				{children && <div className="p-4">{children}</div>}
			</div>
		</div>
	);
}
