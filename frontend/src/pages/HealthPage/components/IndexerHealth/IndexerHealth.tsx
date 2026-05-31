import { AlertTriangle, BarChart3, Radio, RefreshCw, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useConfirm } from "../../../../contexts/ModalContext";
import { useToast } from "../../../../contexts/ToastContext";
import { useCleanupIndexerStats, useIndexerStats } from "../../../../hooks/useApi";
import { IndexerHealthCard } from "./IndexerHealthCard";
import { IndexerHealthChart } from "./IndexerHealthChart";
import { IndexerHealthFilters } from "./IndexerHealthFilters";
import { IndexerHealthSummary } from "./IndexerHealthSummary";
import { PruneStatsModal } from "./PruneStatsModal";
import type { IndexerSummary, SortKey } from "./types";

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
	const [sortKey, setSortKey] = useState<SortKey>("health");
	const [sortAsc, setSortAsc] = useState(false);

	const handlePrune = async (hours: number) => {
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
			setSortAsc(key !== "health");
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

	const summary = useMemo<IndexerSummary | null>(() => {
		if (!stats || stats.length === 0) return null;
		const totalImports = stats.reduce((s, x) => s + x.total_imports, 0);
		const totalSuccess = stats.reduce((s, x) => s + x.success_count, 0);
		const totalFailed = stats.reduce((s, x) => s + x.failed_count, 0);
		const overallRate = totalImports > 0 ? (totalSuccess / totalImports) * 100 : 0;
		const best = [...stats].sort((a, b) => b.success_rate - a.success_rate)[0];
		const worst = [...stats].sort((a, b) => a.success_rate - b.success_rate)[0];
		return { totalImports, totalSuccess, totalFailed, overallRate, best, worst };
	}, [stats]);

	if (isLoading) {
		return (
			<div className="space-y-6" aria-busy="true" aria-live="polite">
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
				<div className="grid grid-cols-2 gap-3 rounded-2xl border border-base-200/40 bg-base-100/10 p-4 sm:grid-cols-4">
					{[...Array(4)].map((_, i) => (
						<div key={i} className="flex flex-col items-center gap-2 py-2">
							<div className="h-8 w-16 animate-pulse rounded bg-base-300/40" />
							<div className="h-3 w-20 animate-pulse rounded bg-base-300/30" />
						</div>
					))}
				</div>
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
			{/* Header */}
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

			{hasStats && showChart && <IndexerHealthChart sorted={sorted} />}

			{summary && <IndexerHealthSummary stats={stats!} summary={summary} />}

			{hasStats && (
				<IndexerHealthFilters
					searchQuery={searchQuery}
					onSearchChange={setSearchQuery}
					statusFilter={statusFilter}
					onStatusFilterChange={setStatusFilter}
					sortKey={sortKey}
					sortAsc={sortAsc}
					onSort={handleSort}
					filteredCount={filtered.length}
				/>
			)}

			{/* Cards Grid */}
			{!hasStats ? (
				<div className="hero rounded-2xl border border-base-300 border-dashed bg-base-200/50 py-16 backdrop-blur-md">
					<div className="hero-content text-center">
						<div className="max-w-md space-y-4">
							<div className="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl border border-teal-500/20 bg-teal-500/5 text-teal-500 shadow-sm dark:text-teal-400">
								<BarChart3 className="h-8 w-8 animate-pulse" aria-hidden="true" />
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
					{filtered.map((item) => (
						<IndexerHealthCard key={item.indexer} item={item} onDelete={handleDeleteIndexer} />
					))}
				</div>
			)}

			{showPruneModal && (
				<PruneStatsModal
					isPending={cleanupStats.isPending}
					onClose={() => setShowPruneModal(false)}
					onPrune={handlePrune}
				/>
			)}
		</div>
	);
}
