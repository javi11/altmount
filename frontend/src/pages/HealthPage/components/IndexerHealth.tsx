import {
	Activity,
	AlertTriangle,
	ArrowUpDown,
	BarChart2,
	BarChart3,
	CheckCircle2,
	Clock,
	Radio,
	RefreshCw,
	Trash2,
	TrendingDown,
	TrendingUp,
	XCircle,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Bar, BarChart, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { useConfirm } from "../../../contexts/ModalContext";
import { useToast } from "../../../contexts/ToastContext";
import { useCleanupIndexerStats, useIndexerStats } from "../../../hooks/useApi";
import { formatRelativeTime } from "../../../lib/utils";

type SortKey = "health" | "total" | "name";

const ChartTooltip = ({
	active,
	payload,
}: {
	active?: boolean;
	payload?: { value: number; payload: { name: string } }[];
}) => {
	if (!active || !payload || payload.length === 0) return null;
	const data = payload[0];
	const val = data.value;
	const name = data.payload.name;

	const isExcellent = val >= 90;
	const isGood = val >= 75 && val < 90;
	const isPoor = val >= 50 && val < 75;
	const statusText = isExcellent
		? "Excellent"
		: isGood
			? "Good"
			: isPoor
				? "Moderate"
				: "Operational";
	const badgeColor = isExcellent
		? "bg-teal-500/10 text-teal-400 border-teal-500/20"
		: isGood
			? "bg-emerald-500/10 text-emerald-400 border-emerald-500/20"
			: isPoor
				? "bg-amber-500/10 text-amber-500 border-amber-500/20"
				: "bg-blue-500/10 text-blue-400 border-blue-500/20";

	return (
		<div className="z-50 rounded-xl border border-base-200 bg-base-100/95 p-3 text-base-content text-xs shadow-2xl backdrop-blur-md">
			<p className="mb-1.5 font-extrabold leading-tight">{name}</p>
			<div className="flex items-center gap-2">
				<span
					className={`badge badge-xs border ${badgeColor} py-1.5 font-bold uppercase tracking-wider`}
				>
					{statusText}
				</span>
				<span className="font-extrabold font-mono text-sm">{val.toFixed(1)}%</span>
			</div>
		</div>
	);
};

export function IndexerHealth() {
	const { data: stats, isLoading, error, refetch } = useIndexerStats();
	const cleanupStats = useCleanupIndexerStats();
	const { showToast } = useToast();
	const { confirmAction } = useConfirm();

	const [showChart, setShowChart] = useState(false);
	const [searchQuery, setSearchQuery] = useState("");
	const [statusFilter, setStatusFilter] = useState<
		"all" | "excellent" | "good" | "moderate" | "operational"
	>("all");

	const [showPruneModal, setShowPruneModal] = useState(false);
	const [pruneOption, setPruneOption] = useState<"24h" | "7d" | "30d" | "custom">("24h");
	const [customDays, setCustomDays] = useState(3);
	const [sortKey, setSortKey] = useState<SortKey>("health");
	const [sortAsc, setSortAsc] = useState(false); // health: best (high %) first by default

	const handlePrune = async () => {
		let hours = 24;
		let label = "Last 24 Hours";

		if (pruneOption === "7d") {
			hours = 7 * 24;
			label = "Last 7 Days";
		} else if (pruneOption === "30d") {
			hours = 30 * 24;
			label = "Last 30 Days";
		} else if (pruneOption === "custom") {
			if (customDays <= 0) {
				showToast({
					title: "Invalid Input",
					message: "Please enter a positive number of days",
					type: "error",
				});
				return;
			}
			hours = customDays * 24;
			label = `Last ${customDays} Days`;
		}

		const confirmed = await confirmAction(
			"Prune Indexer Statistics",
			`Are you sure you want to delete all logged indexer statistics from the ${label}? This cannot be undone.`,
			{ type: "warning", confirmText: "Prune Data", confirmButtonClass: "btn-warning" },
		);
		if (!confirmed) return;

		try {
			const res = await cleanupStats.mutateAsync({ hours });
			showToast({
				title: "Stats Pruned",
				message: `Successfully pruned ${res.pruned_rows} statistics records.`,
				type: "success",
			});
			setShowPruneModal(false);
			void refetch();
		} catch (err) {
			console.error("Failed to prune indexer stats:", err);
			showToast({
				title: "Pruning Failed",
				message: "An error occurred while pruning indexer statistics.",
				type: "error",
			});
		}
	};

	const handleDeleteIndexer = async (indexer: string) => {
		const confirmed = await confirmAction(
			"Delete Indexer Stats",
			`Are you sure you want to delete all statistics for "${indexer}"? This action cannot be undone.`,
			{
				type: "error",
				confirmText: "Delete",
				confirmButtonClass: "btn-error",
				verificationText: indexer,
			},
		);
		if (!confirmed) return;
		try {
			await cleanupStats.mutateAsync({ indexer });
			showToast({
				title: "Indexer Stats Deleted",
				message: `Successfully deleted statistics for ${indexer}.`,
				type: "success",
			});
			void refetch();
		} catch {
			showToast({
				title: "Delete Failed",
				message: `Failed to delete stats for ${indexer}.`,
				type: "error",
			});
		}
	};

	const handleSort = (key: SortKey) => {
		if (sortKey === key) {
			setSortAsc((a) => !a);
		} else {
			setSortKey(key);
			setSortAsc(key !== "health"); // health: best (high %) first = desc = false
		}
	};

	const sorted = useMemo(() => {
		if (!stats) return [];
		return [...stats].sort((a, b) => {
			let cmp = 0;
			if (sortKey === "health") cmp = a.success_rate - b.success_rate;
			else if (sortKey === "total") cmp = a.total_imports - b.total_imports;
			else cmp = a.indexer.localeCompare(b.indexer);
			return sortAsc ? cmp : -cmp;
		});
	}, [stats, sortKey, sortAsc]);

	const filtered = useMemo(() => {
		return sorted.filter((item) => {
			const matchesSearch = item.indexer.toLowerCase().includes(searchQuery.toLowerCase());
			const rate = item.success_rate;
			let matchesFilter = true;
			if (statusFilter === "excellent") matchesFilter = rate >= 90;
			else if (statusFilter === "good") matchesFilter = rate >= 75 && rate < 90;
			else if (statusFilter === "moderate") matchesFilter = rate >= 50 && rate < 75;
			else if (statusFilter === "operational") matchesFilter = rate < 50;
			return matchesSearch && matchesFilter;
		});
	}, [sorted, searchQuery, statusFilter]);

	// Aggregate summary stats
	const summary = useMemo(() => {
		if (!stats || stats.length === 0) return null;
		const totalImports = stats.reduce((s, x) => s + x.total_imports, 0);
		const totalSuccess = stats.reduce((s, x) => s + x.success_count, 0);
		const totalFailed = stats.reduce((s, x) => s + x.failed_count, 0);
		const overallRate = totalImports > 0 ? (totalSuccess / totalImports) * 100 : 0;
		const best = [...stats].sort((a, b) => b.success_rate - a.success_rate)[0];
		const worst = [...stats].sort((a, b) => a.success_rate - b.success_rate)[0];
		return { totalImports, totalSuccess, totalFailed, overallRate, best, worst };
	}, [stats]);

	// Generates dynamic GitHub-style Import Pulse dot matrices
	const generatePulseMatrix = (success: number, _failed: number, total: number) => {
		const dots: ("success" | "failed" | "neutral")[] = [];
		const totalAvailable = Math.min(24, total);

		const successRatio = total > 0 ? success / total : 0;
		const successDotsCount = Math.round(totalAvailable * successRatio);
		const failedDotsCount = totalAvailable - successDotsCount;

		for (let i = 0; i < totalAvailable; i++) {
			if (failedDotsCount > 0 && (i * failedDotsCount) % totalAvailable < failedDotsCount) {
				dots.push("failed");
			} else {
				dots.push("success");
			}
		}

		while (dots.length < 24) {
			dots.push("neutral");
		}
		return dots.reverse();
	};

	if (isLoading) {
		return (
			<div className="space-y-6" aria-busy="true" aria-live="polite">
				{/* Header Skeleton */}
				<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
					<div className="space-y-2">
						<div className="h-7 w-48 animate-pulse rounded-lg bg-base-300/40" />
						<div className="h-4 w-80 animate-pulse rounded-lg bg-base-300/30" />
					</div>
					<div className="flex gap-2">
						<div className="h-8 w-24 animate-pulse rounded-lg bg-base-300/40" />
						<div className="h-8 w-20 animate-pulse rounded-lg bg-base-300/40" />
					</div>
				</div>

				{/* Summary Stats Skeleton */}
				<div className="grid grid-cols-2 gap-3 rounded-2xl border border-base-200/40 bg-base-100/10 p-4 sm:grid-cols-4">
					{[...Array(4)].map((_, i) => (
						<div key={i} className="flex flex-col items-center gap-2 py-2">
							<div className="h-8 w-16 animate-pulse rounded bg-base-300/40" />
							<div className="h-3 w-20 animate-pulse rounded bg-base-300/30" />
						</div>
					))}
				</div>

				{/* Cards Skeleton Grid */}
				<div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
					{[...Array(3)].map((_, i) => (
						<div
							key={i}
							className="card border border-base-200/30 bg-base-100/20 p-5 shadow backdrop-blur-md"
						>
							<div className="flex justify-between gap-4">
								<div className="flex-1 space-y-2">
									<div className="h-5 w-32 animate-pulse rounded bg-base-300/40" />
									<div className="h-3 w-24 animate-pulse rounded bg-base-300/30" />
								</div>
								<div className="h-6 w-6 animate-pulse rounded-full bg-base-300/40" />
							</div>
							<div className="mt-4 h-8 w-20 animate-pulse rounded bg-base-300/40" />
							<div className="mt-3 space-y-2">
								<div className="h-2.5 w-full animate-pulse rounded bg-base-300/40" />
								<div className="h-3 w-full animate-pulse rounded bg-base-300/30" />
							</div>
							<div className="mt-4 h-16 w-full animate-pulse rounded-xl bg-base-300/40" />
						</div>
					))}
				</div>
			</div>
		);
	}

	if (error) {
		return (
			<div className="alert alert-error shadow-lg" role="alert">
				<AlertTriangle className="h-6 w-6 shrink-0" aria-hidden="true" />
				<div>
					<h3 className="font-bold">Error Loading Statistics</h3>
					<div className="text-xs">Failed to load persistent indexer import history.</div>
				</div>
			</div>
		);
	}

	const hasStats = stats && stats.length > 0;

	return (
		<div className="space-y-6">
			{/* ── Top Header ── */}
			<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
				<div>
					<h3 className="flex items-center gap-2 font-extrabold text-base-content text-lg tracking-tight">
						<Radio
							className="h-5 w-5 animate-pulse text-teal-500 dark:text-teal-400"
							aria-hidden="true"
						/>
						Usenet Indexers Health HUD
					</h3>
					<p className="font-medium text-base-content/50 text-xs sm:text-sm">
						Persistent success and failure rates tracked per indexer via webhook handshake.
					</p>
				</div>
				<div className="flex flex-wrap items-center gap-2">
					<button
						type="button"
						className={`btn btn-sm gap-1.5 border-base-200 transition-all duration-200 ${
							showChart
								? "btn-primary shadow-[0_0_12px_rgba(59,130,246,0.3)]"
								: "btn-ghost border border-base-200 bg-base-200/50 hover:bg-base-200"
						}`}
						onClick={() => setShowChart(!showChart)}
						disabled={!hasStats}
						aria-label="Toggle success benchmark comparative analytics chart"
					>
						<BarChart3 className="h-4 w-4" aria-hidden="true" />
						{showChart ? "Hide HUD Chart" : "Show HUD Chart"}
					</button>
					<button
						type="button"
						className="btn btn-ghost btn-sm gap-1.5 border border-base-200 bg-base-200/50 transition-all duration-200 hover:scale-[1.02] hover:bg-base-200 active:scale-[0.98]"
						onClick={() => void refetch()}
						aria-label="Refresh indexer statistics"
					>
						<RefreshCw className="h-4 w-4" aria-hidden="true" />
						Sync HUD
					</button>
					<button
						type="button"
						className="btn btn-warning btn-sm gap-1.5 shadow-[0_2px_12px_rgba(217,119,6,0.2)] transition-all duration-200"
						onClick={() => setShowPruneModal(true)}
						disabled={!hasStats}
						aria-label="Prune indexer statistics history"
					>
						<Trash2 className="h-4 w-4" aria-hidden="true" />
						Prune Stats
					</button>
				</div>
			</div>

			{/* ── Collapsible Comparative Success Benchmark Chart ── */}
			{hasStats && showChart && (
				<div className="card overflow-hidden border border-base-200 bg-base-100/60 p-5 shadow-xl backdrop-blur-md transition-all duration-300">
					<div className="mb-4 flex items-center justify-between border-base-200 border-b pb-2">
						<div>
							<h4 className="flex items-center gap-2 font-bold text-base-content text-sm">
								<BarChart2
									className="h-4 w-4 animate-pulse text-teal-500 dark:text-teal-400"
									aria-hidden="true"
								/>
								Usenet Indexer Success Comparison
							</h4>
							<p className="font-medium text-[10px] text-base-content/50">
								Comparative efficiency rating across active indexers (%)
							</p>
						</div>
					</div>
					<div className="h-64 w-full">
						<ResponsiveContainer width="100%" height="100%">
							<BarChart
								data={sorted.map((s) => ({
									name: s.indexer,
									health: Math.round(s.success_rate * 10) / 10,
								}))}
								margin={{ top: 10, right: 10, left: -20, bottom: 5 }}
							>
								<XAxis
									dataKey="name"
									stroke="currentColor"
									className="text-[10px] text-base-content/40"
									tickLine={false}
									axisLine={false}
								/>
								<YAxis
									domain={[0, 100]}
									stroke="currentColor"
									className="text-[10px] text-base-content/40"
									tickLine={false}
									axisLine={false}
									tickFormatter={(v) => `${v}%`}
								/>
								<Tooltip
									content={<ChartTooltip />}
									cursor={{ fill: "rgba(255, 255, 255, 0.05)", radius: 4 }}
								/>
								<Bar dataKey="health" radius={[4, 4, 0, 0]} barSize={36}>
									{sorted.map((entry, index) => {
										const isExcellent = entry.success_rate >= 90;
										const isGood = entry.success_rate >= 75 && entry.success_rate < 90;
										const isPoor = entry.success_rate >= 50 && entry.success_rate < 75;
										const color = isExcellent
											? "#0d9488"
											: isGood
												? "#059669"
												: isPoor
													? "#d97706"
													: "#e11d48";
										return <Cell key={`cell-${index}`} fill={color} />;
									})}
								</Bar>
							</BarChart>
						</ResponsiveContainer>
					</div>
				</div>
			)}

			{/* ── Summary Banner ── */}
			{summary && (
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
					{/* Total Indexers Card */}
					<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-teal-500/20">
						<div className="relative z-10 space-y-1">
							<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
								Tracked Indexers
							</span>
							<div className="font-extrabold font-mono text-2xl text-teal-600 tracking-tight dark:text-teal-400">
								{stats?.length ?? 0}
							</div>
							<div className="font-semibold text-[10px] text-base-content/50">
								Active Integrations
							</div>
						</div>
						<div className="relative z-10 text-teal-500 dark:text-teal-400">
							<Activity className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
						</div>
					</div>

					{/* Overall Health Card */}
					<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-primary/20">
						{summary.overallRate >= 85 && (
							<div className="absolute inset-0 animate-pulse bg-gradient-to-tr from-teal-500/5 via-transparent to-transparent opacity-60" />
						)}
						<div className="relative z-10 space-y-1">
							<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
								System Health
							</span>
							<div
								className={`font-black font-mono text-3xl tracking-tight ${
									summary.overallRate >= 85
										? "text-teal-600 dark:text-teal-400"
										: summary.overallRate >= 60
											? "text-amber-600 dark:text-amber-500"
											: "text-rose-600 dark:text-rose-400"
								}`}
							>
								{summary.overallRate.toFixed(1)}%
							</div>
							<div className="font-semibold text-[10px] text-base-content/50">
								Average success rate
							</div>
						</div>
						<div
							className={`relative z-10 ${
								summary.overallRate >= 85
									? "text-teal-600 shadow-[0_0_12px_rgba(13,148,136,0.3)] dark:text-teal-400"
									: summary.overallRate >= 60
										? "text-amber-600 shadow-[0_0_12px_rgba(245,158,11,0.3)] dark:text-amber-500"
										: "text-rose-600 shadow-[0_0_12px_rgba(225,29,72,0.3)] dark:text-rose-400"
							}`}
						>
							<BarChart2 className="h-8 w-8 opacity-50" />
						</div>
					</div>

					{/* Successful Imports Card */}
					<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-emerald-500/20">
						<div className="relative z-10 space-y-1">
							<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
								Successful Imports
							</span>
							<div className="font-extrabold font-mono text-2xl text-emerald-600 tracking-tight dark:text-emerald-400">
								{summary.totalSuccess.toLocaleString()}
							</div>
							<div className="font-semibold text-[10px] text-base-content/50">
								Imports completed
							</div>
						</div>
						<div className="relative z-10 text-emerald-600 dark:text-emerald-400">
							<CheckCircle2 className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
						</div>
					</div>

					{/* Failed Imports Card */}
					<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-rose-500/20">
						<div className="relative z-10 space-y-1">
							<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
								Failed Imports
							</span>
							<div className="font-extrabold font-mono text-2xl text-rose-600 tracking-tight dark:text-rose-400">
								{summary.totalFailed.toLocaleString()}
							</div>
							<div className="font-semibold text-[10px] text-base-content/50">
								Verification failures
							</div>
						</div>
						<div className="relative z-10 text-rose-600 dark:text-rose-400">
							<XCircle className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
						</div>
					</div>

					{/* Best / Worst performer chips */}
					{stats && stats.length > 1 && (
						<>
							<div className="col-span-1 flex items-center gap-3 rounded-xl border border-teal-500/10 bg-teal-500/5 px-3 py-2.5 transition-all duration-300 hover:bg-teal-500/10 md:col-span-2">
								<TrendingUp className="h-4 w-4 shrink-0 text-teal-400" aria-hidden="true" />
								<div className="min-w-0">
									<div className="truncate font-bold text-teal-400 text-xs">
										{summary.best.indexer}
									</div>
									<div className="mt-0.5 font-semibold text-[10px] text-base-content/50">
										Highest efficiency rating · {summary.best.success_rate.toFixed(1)}%
									</div>
								</div>
							</div>
							<div className="col-span-1 flex items-center gap-3 rounded-xl border border-rose-500/10 bg-rose-500/5 px-3 py-2.5 transition-all duration-300 hover:bg-rose-500/10 md:col-span-2">
								<TrendingDown className="h-4 w-4 shrink-0 text-rose-400" aria-hidden="true" />
								<div className="min-w-0">
									<div className="truncate font-bold text-rose-400 text-xs">
										{summary.worst.indexer}
									</div>
									<div className="mt-0.5 font-semibold text-[10px] text-base-content/50">
										Needs telemetry inspection · {summary.worst.success_rate.toFixed(1)}%
									</div>
								</div>
							</div>
						</>
					)}
				</div>
			)}

			{/* ── Search & Filter Toolbar ── */}
			{hasStats && (
				<div className="flex flex-col gap-3 rounded-2xl border border-base-200 bg-base-100 p-3 backdrop-blur-md md:flex-row md:items-center md:justify-between">
					{/* Search Input */}
					<div className="relative max-w-sm flex-1">
						<input
							type="text"
							placeholder="Search indexers..."
							value={searchQuery}
							onChange={(e) => setSearchQuery(e.target.value)}
							className="input input-bordered input-sm w-full border-base-300 bg-base-200/50 pl-8 font-medium text-base-content placeholder-base-content/40 focus:border-teal-500/50"
							aria-label="Search indexers"
						/>
						<div className="absolute top-1/2 left-2.5 -translate-y-1/2 text-base-content/40">
							<svg
								xmlns="http://www.w3.org/2000/svg"
								className="h-4 w-4"
								fill="none"
								viewBox="0 0 24 24"
								stroke="currentColor"
								strokeWidth={2}
								aria-hidden="true"
							>
								<path
									strokeLinecap="round"
									strokeLinejoin="round"
									d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
								/>
							</svg>
						</div>
					</div>

					{/* Status Filter Chips */}
					<div
						className="flex flex-wrap items-center gap-1.5"
						role="group"
						aria-label="Status Filters"
					>
						<span className="mr-1 font-bold text-[10px] text-base-content/40 uppercase tracking-wider">
							Filter
						</span>
						{(["all", "excellent", "good", "moderate", "operational"] as const).map((filter) => {
							const active = statusFilter === filter;
							let btnClass =
								"btn-ghost text-base-content/60 hover:text-base-content hover:bg-base-content/5 border-transparent";
							if (active) {
								if (filter === "excellent")
									btnClass =
										"bg-teal-500/15 border-teal-500/30 text-teal-400 shadow-[0_0_8px_rgba(20,184,166,0.25)]";
								else if (filter === "good")
									btnClass =
										"bg-emerald-500/15 border-emerald-500/30 text-emerald-400 shadow-[0_0_8px_rgba(16,185,129,0.25)]";
								else if (filter === "moderate")
									btnClass =
										"bg-amber-500/15 border-amber-500/30 text-amber-500 shadow-[0_0_8px_rgba(245,158,11,0.25)]";
								else if (filter === "operational")
									btnClass =
										"bg-slate-500/15 border-slate-500/30 text-slate-400 shadow-[0_0_8px_rgba(148,163,184,0.25)]";
								else
									btnClass =
										"bg-primary/15 border-primary/30 text-primary shadow-[0_0_8px_rgba(59,130,246,0.25)]";
							}
							return (
								<button
									key={filter}
									type="button"
									onClick={() => setStatusFilter(filter)}
									className={`btn btn-xs rounded-lg border font-bold capitalize tracking-wide transition-all duration-200 ${btnClass}`}
								>
									{filter}
								</button>
							);
						})}
					</div>
				</div>
			)}

			{/* ── Sort Toolbar ── */}
			{hasStats && (
				<div className="flex items-center gap-3">
					<span className="font-bold text-[10px] text-base-content/50 uppercase tracking-wider">
						Sort by
					</span>
					<div
						className="join rounded-xl border border-base-200 bg-base-200/30 p-0.5"
						role="group"
						aria-label="Sort options"
					>
						{(["health", "total", "name"] as SortKey[]).map((key) => (
							<button
								key={key}
								type="button"
								onClick={() => handleSort(key)}
								className={`btn btn-xs join-item border-none font-bold capitalize tracking-wide transition-all duration-200 ${
									sortKey === key
										? "btn-primary shadow-[0_0_8px_rgba(59,130,246,0.25)]"
										: "btn-ghost text-base-content/60 hover:bg-base-content/5 hover:text-base-content"
								}`}
								aria-label={`Sort by ${key === "health" ? "Health" : key === "total" ? "Volume" : "Name"}`}
							>
								{key === "health" ? "Health %" : key === "total" ? "Volume" : "Name"}
								{sortKey === key && (
									<ArrowUpDown className="ml-1 h-3 w-3 transition-transform" aria-hidden="true" />
								)}
							</button>
						))}
					</div>
					<span className="ml-auto font-semibold text-base-content/40 text-xs">
						{filtered.length} Indexer{filtered.length !== 1 ? "s" : ""} active
					</span>
				</div>
			)}

			{/* ── Cards Grid ── */}
			{!hasStats ? (
				<div className="hero rounded-2xl border border-base-300 border-dashed bg-base-200/50 py-16 backdrop-blur-md">
					<div className="hero-content text-center">
						<div className="max-w-md space-y-4">
							<div className="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl border border-teal-500/20 bg-teal-500/5 text-teal-500 shadow-sm dark:text-teal-400">
								<BarChart2 className="h-8 w-8 animate-pulse" aria-hidden="true" />
							</div>
							<h3 className="font-bold text-base-content text-xl tracking-tight">
								No Indexer History Yet
							</h3>
							<p className="text-base-content/50 text-sm">
								Success and failure statistics will populate automatically as active imports
								finalize in the queue. Configure indexer webhooks to start tracking!
							</p>
						</div>
					</div>
				</div>
			) : filtered.length === 0 ? (
				<div className="hero rounded-2xl border border-base-300 border-dashed bg-base-200/50 py-12 text-center">
					<div className="max-w-xs space-y-2">
						<h3 className="font-bold text-base text-base-content/70">No Indexers Found</h3>
						<p className="text-base-content/40 text-xs">
							Try adjusting your fuzzy search or status filter options.
						</p>
					</div>
				</div>
			) : (
				<div
					className={`grid gap-5 ${
						filtered.length === 1
							? "max-w-md"
							: filtered.length === 2
								? "max-w-4xl sm:grid-cols-2"
								: "sm:grid-cols-2 lg:grid-cols-3"
					}`}
				>
					{filtered.map((item) => {
						const isExcellent = item.success_rate >= 90;
						const isGood = item.success_rate >= 75 && item.success_rate < 90;
						const isPoor = item.success_rate >= 50 && item.success_rate < 75;

						// Glowing border card aesthetics in dark-slate frosted HUD theme
						const accentColor = isExcellent
							? "border-teal-500/15 hover:border-teal-500/40 hover:shadow-[0_0_15px_rgba(20,184,166,0.1)]"
							: isGood
								? "border-emerald-500/15 hover:border-emerald-500/40 hover:shadow-[0_0_15px_rgba(16,185,129,0.1)]"
								: isPoor
									? "border-amber-500/15 hover:border-amber-500/40 hover:shadow-[0_0_15px_rgba(245,158,11,0.1)]"
									: "border-slate-500/15 hover:border-slate-500/40 hover:shadow-[0_0_15px_rgba(148,163,184,0.1)]";

						const barSuccessWidth =
							item.total_imports > 0 ? (item.success_count / item.total_imports) * 100 : 0;
						const barFailWidth =
							item.total_imports > 0 ? (item.failed_count / item.total_imports) * 100 : 0;

						const topLineGradient = isExcellent
							? "from-teal-500/40 to-teal-600/10"
							: isGood
								? "from-emerald-500/40 to-emerald-600/10"
								: isPoor
									? "from-amber-500/40 to-amber-600/10"
									: "from-slate-500/40 to-slate-600/10";

						const statusBadgeColor = isExcellent
							? "bg-teal-500/10 text-teal-400 border-teal-500/20"
							: isGood
								? "bg-emerald-500/10 text-emerald-400 border-emerald-500/20"
								: isPoor
									? "bg-amber-500/10 text-amber-500 border-amber-500/20"
									: "bg-slate-500/10 text-slate-400 border-slate-500/20";

						const statusText = isExcellent
							? "EXCELLENT"
							: isGood
								? "GOOD"
								: isPoor
									? "MODERATE"
									: "OPERATIONAL";

						return (
							<div
								key={item.indexer}
								className={`group card relative overflow-hidden border ${accentColor} bg-base-100/60 shadow-md backdrop-blur-md transition-all duration-500 ease-out hover:-translate-y-1.5 hover:scale-[1.01]`}
							>
								{/* Premium matte glowing top bar */}
								<div
									className={`absolute top-0 right-0 left-0 h-[2px] bg-gradient-to-r ${topLineGradient}`}
								/>

								{/* Delete Indexer Stats Action absolute positioned */}
								<div className="absolute top-3 right-3 z-10">
									<button
										type="button"
										className="btn btn-ghost btn-xs p-1 text-rose-500 opacity-0 transition-all duration-200 hover:bg-rose-500/20 hover:text-rose-600 group-hover:opacity-100 dark:hover:text-rose-300"
										onClick={() => handleDeleteIndexer(item.indexer)}
										aria-label={`Delete statistics for ${item.indexer}`}
									>
										<Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
									</button>
								</div>

								<div className="card-body p-5">
									{/* Header row with details and maximized percentage */}
									<div className="flex items-center justify-between gap-3">
										{/* Details */}
										<div className="min-w-0 flex-1 space-y-2 py-0.5">
											<h4 className="truncate pr-6 font-extrabold text-[17px] text-base-content leading-tight tracking-tight sm:text-lg">
												{item.indexer}
											</h4>
											<div className="flex flex-wrap items-center gap-2">
												<span
													className={`badge badge-xs border ${statusBadgeColor} py-2 font-black text-[9px] tracking-wider`}
												>
													{statusText}
												</span>
												<div className="flex items-center gap-1 font-semibold text-[10px] text-base-content/40">
													<Clock className="h-2.5 w-2.5 shrink-0" aria-hidden="true" />
													<span>Seen {formatRelativeTime(item.last_seen_at)}</span>
												</div>
											</div>
										</div>

										{/* Maximized Percentage on the Right */}
										<div className="flex shrink-0 flex-col items-end pl-2">
											<span
												className={`flex items-baseline font-black font-mono text-[17px] leading-none tracking-tight sm:text-lg ${
													isExcellent
														? "text-teal-600 dark:text-teal-400"
														: isGood
															? "text-emerald-600 dark:text-emerald-400"
															: isPoor
																? "text-amber-600 dark:text-amber-500"
																: "text-slate-600 dark:text-slate-400"
												}`}
											>
												{item.success_rate.toFixed(1)}
												<span className="ml-0.5 font-semibold text-[9px] opacity-50 sm:text-[10px]">
													%
												</span>
											</span>
											<span className="mt-1.5 font-black text-[8px] text-base-content/40 uppercase tracking-widest">
												SUCCESS
											</span>
										</div>
									</div>

									{/* Sleek Gradient Split progress bar */}
									<div className="mt-4 space-y-1">
										<div className="relative flex h-2 w-full overflow-hidden rounded-full border border-base-200 bg-base-200/40">
											<div
												className="h-full bg-gradient-to-r from-teal-500/80 to-teal-400/80 shadow-[0_0_8px_rgba(20,184,166,0.3)] transition-all duration-700"
												style={{ width: `${barSuccessWidth}%` }}
												role="progressbar"
												aria-valuenow={barSuccessWidth}
												aria-valuemin={0}
												aria-valuemax={100}
												aria-label="Success rate percentage"
											/>
											<div
												className="h-full bg-gradient-to-r from-rose-500/80 to-pink-600/80 shadow-[0_0_8px_rgba(239,68,68,0.3)] transition-all duration-700"
												style={{ width: `${barFailWidth}%` }}
												role="progressbar"
												aria-valuenow={barFailWidth}
												aria-valuemin={0}
												aria-valuemax={100}
												aria-label="Failure rate percentage"
											/>
										</div>
										<div className="flex justify-between font-bold text-[9px] uppercase tracking-wide">
											<span className="text-teal-400/90">{item.success_count} OK</span>
											<span className="text-rose-400/90">{item.failed_count} FAILED</span>
										</div>
									</div>

									{/* GitHub-style Dot Matrix Import Pulse Stream */}
									<div className="mt-4 space-y-1.5">
										<div className="flex items-center gap-1.5 font-bold text-[9px] text-base-content/40 uppercase tracking-wider">
											<Radio className="h-3 w-3 animate-pulse text-teal-400" />
											Import Pulse Stream (Last 24)
										</div>
										<div className="flex flex-wrap items-center gap-1 rounded-xl border border-base-200 bg-base-200/50 p-2">
											{generatePulseMatrix(
												item.success_count,
												item.failed_count,
												item.total_imports,
											).map((dot, idx) => {
												const dotColor =
													dot === "success"
														? "bg-teal-500/80 hover:bg-teal-400 shadow-[0_0_6px_rgba(20,184,166,0.4)]"
														: dot === "failed"
															? "bg-rose-500/80 hover:bg-rose-400 shadow-[0_0_6px_rgba(239,68,68,0.4)]"
															: "bg-base-200/30 border border-base-200";
												const dotTip =
													dot === "success"
														? "Import OK"
														: dot === "failed"
															? "Import Failed"
															: "No Activity";
												return (
													<div
														key={idx}
														className={`h-2.5 w-2.5 cursor-pointer rounded-sm transition-all duration-300 hover:scale-125 ${dotColor} tooltip tooltip-top`}
														data-tip={dotTip}
													/>
												);
											})}
										</div>
									</div>

									{/* Telemetry Grid */}
									<div className="mt-4 grid grid-cols-3 gap-1.5 rounded-xl border border-base-200 bg-base-200/50 p-3 text-center">
										<div className="space-y-0.5">
											<div className="font-extrabold text-base-content text-sm tabular-nums">
												{item.total_imports.toLocaleString()}
											</div>
											<div className="font-bold text-[8px] text-base-content/40 uppercase tracking-wider">
												Total
											</div>
										</div>
										<div className="space-y-0.5">
											<div className="flex items-center justify-center gap-0.5 font-extrabold text-sm text-teal-600 tabular-nums dark:text-teal-400">
												<CheckCircle2 className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
												{item.success_count.toLocaleString()}
											</div>
											<div className="font-bold text-[8px] text-base-content/40 uppercase tracking-wider">
												Success
											</div>
										</div>
										<div className="space-y-0.5">
											<div className="flex items-center justify-center gap-0.5 font-extrabold text-rose-600 text-sm tabular-nums dark:text-rose-400">
												<XCircle className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
												{item.failed_count.toLocaleString()}
											</div>
											<div className="font-bold text-[8px] text-base-content/40 uppercase tracking-wider">
												Failed
											</div>
										</div>
									</div>
								</div>
							</div>
						);
					})}
				</div>
			)}

			{/* ── Prune Modal ── */}
			{showPruneModal && (
				<div
					className="modal modal-open backdrop-blur-sm"
					role="dialog"
					aria-modal="true"
					aria-labelledby="prune-modal-title"
				>
					<div className="modal-box max-w-md border border-base-300 bg-base-100 p-6 shadow-2xl sm:p-8">
						<h3
							id="prune-modal-title"
							className="flex items-center gap-2 font-bold text-base-content text-xl"
						>
							<Trash2 className="h-6 w-6 text-amber-500" aria-hidden="true" />
							Prune Statistics
						</h3>
						<p className="py-4 font-medium text-base-content/60 text-sm">
							Choose the time period of historical statistics you would like to clear.
						</p>

						<div className="space-y-3">
							{(["24h", "7d", "30d", "custom"] as const).map((opt) => {
								const isSelected = pruneOption === opt;
								return (
									<label
										key={opt}
										className={`label cursor-pointer justify-start gap-3 rounded-xl border p-4 transition-all duration-200 hover:bg-base-200 ${
											isSelected
												? "border-primary bg-primary/5 shadow-sm"
												: "border-base-200 bg-base-200/30"
										}`}
									>
										<input
											type="radio"
											name="prune_option"
											className="radio radio-primary"
											checked={isSelected}
											onChange={() => setPruneOption(opt)}
											aria-label={`Prune period: ${
												opt === "24h"
													? "24 Hours"
													: opt === "7d"
														? "7 Days"
														: opt === "30d"
															? "30 Days"
															: "Custom Days"
											}`}
										/>
										<div className="flex-1">
											<span className="font-bold text-base-content text-sm">
												{opt === "24h"
													? "Delete Last 24 Hours"
													: opt === "7d"
														? "Delete Last 7 Days"
														: opt === "30d"
															? "Delete Last 30 Days"
															: "Delete Custom Period"}
											</span>
											<p className="mt-0.5 font-medium text-[10px] text-base-content/50 sm:text-xs">
												{opt === "24h"
													? "Resets statistics from the most recent day only."
													: opt === "7d"
														? "Resets the last week of collected indexer data."
														: opt === "30d"
															? "Clears the past month of statistics."
															: "Specify a custom number of days to clear."}
											</p>
											{opt === "custom" && isSelected && (
												<div className="mt-3 flex items-center gap-3">
													<input
														type="number"
														className="input input-bordered input-sm w-24 border-base-300 bg-base-100 text-center font-bold text-base-content"
														value={customDays}
														onChange={(e) =>
															setCustomDays(Number.parseInt(e.target.value, 10) || 0)
														}
														min="1"
														aria-label="Custom prune period in days"
													/>
													<span className="font-bold text-base-content/60 text-xs">
														Days of data
													</span>
												</div>
											)}
										</div>
									</label>
								);
							})}
						</div>

						<div className="modal-action mt-6 gap-2">
							<button
								type="button"
								className="btn btn-ghost text-base-content/70 hover:text-base-content"
								onClick={() => setShowPruneModal(false)}
								disabled={cleanupStats.isPending}
							>
								Cancel
							</button>
							<button
								type="button"
								className="btn btn-warning gap-2 shadow-[0_2px_12px_rgba(217,119,6,0.2)] transition-all duration-200"
								onClick={handlePrune}
								disabled={cleanupStats.isPending}
							>
								{cleanupStats.isPending && (
									<RefreshCw className="h-4 w-4 animate-spin" aria-hidden="true" />
								)}
								Prune Statistics
							</button>
						</div>
					</div>
				</div>
			)}
		</div>
	);
}
