import { AlertTriangle, BarChart3, Radio, RefreshCw, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useConfirm } from "../../../contexts/ModalContext";
import { useToast } from "../../../contexts/ToastContext";
import { useCleanupIndexerStats, useIndexerStats } from "../../../hooks/useApi";
import { IndexerHealthFilters } from "./IndexerHealthFilters";
import { IndexerHealthSummary } from "./IndexerHealthSummary";
import { IndexerHealthTable } from "./IndexerHealthTable";
import { PruneStatsModal } from "./PruneStatsModal";
import { type IndexerSummary, SORT_KEYS, type SortKey } from "./types";

const SORT_STORAGE_KEY = "altmount.indexerHealth.sort";

// Persisted sort preference so the chosen order survives a page refresh instead
// of resetting to health-descending.
function loadSortPref(): { key: SortKey; asc: boolean } {
	try {
		const raw = localStorage.getItem(SORT_STORAGE_KEY);
		if (raw) {
			const p = JSON.parse(raw) as { key?: unknown; asc?: unknown };
			const key: SortKey =
				typeof p.key === "string" && (SORT_KEYS as string[]).includes(p.key)
					? (p.key as SortKey)
					: "health";
			const asc = typeof p.asc === "boolean" ? p.asc : false;
			return { key, asc };
		}
	} catch {
		// ignore malformed/unavailable storage
	}
	return { key: "health", asc: false };
}

export function IndexerHealth() {
	const { data: stats, isLoading, error, refetch } = useIndexerStats();
	const cleanupStats = useCleanupIndexerStats();
	const { showToast } = useToast();
	const { confirmAction } = useConfirm();

	const [searchQuery, setSearchQuery] = useState("");
	const [statusFilter, setStatusFilter] = useState<"all" | "excellent" | "moderate" | "poor">(
		"all",
	);
	const [showPruneModal, setShowPruneModal] = useState(false);
	const [sortKey, setSortKey] = useState<SortKey>(() => loadSortPref().key);
	const [sortAsc, setSortAsc] = useState(() => loadSortPref().asc);

	useEffect(() => {
		try {
			localStorage.setItem(SORT_STORAGE_KEY, JSON.stringify({ key: sortKey, asc: sortAsc }));
		} catch {
			// ignore storage write failures
		}
	}, [sortKey, sortAsc]);

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
			setSortAsc(!sortAsc);
		} else {
			setSortKey(key);
			// Text column defaults to ascending; numeric/time columns to descending.
			setSortAsc(key === "name");
		}
	};

	const sorted = useMemo(() => {
		if (!stats) return [];
		return [...stats].sort((a, b) => {
			let cmp = 0;
			switch (sortKey) {
				case "name":
					cmp = a.indexer.localeCompare(b.indexer);
					break;
				case "last_24h":
					cmp = (a.last_24h_count ?? 0) - (b.last_24h_count ?? 0);
					break;
				case "last_seen":
					cmp = new Date(a.last_seen_at).getTime() - new Date(b.last_seen_at).getTime();
					break;
				case "success":
					cmp = a.success_count - b.success_count;
					break;
				case "failed":
					cmp = a.failed_count - b.failed_count;
					break;
				case "total":
					cmp = a.total_imports - b.total_imports;
					break;
				default:
					cmp = a.success_rate - b.success_rate;
					break;
			}
			return sortAsc ? cmp : -cmp;
		});
	}, [stats, sortKey, sortAsc]);

	const filtered = useMemo(() => {
		return sorted.filter((item) => {
			const matchesSearch = item.indexer.toLowerCase().includes(searchQuery.toLowerCase());
			const rate = item.success_rate;
			let matchesFilter = true;
			if (statusFilter === "excellent") matchesFilter = rate >= 85;
			else if (statusFilter === "moderate") matchesFilter = rate >= 50 && rate < 85;
			else if (statusFilter === "poor") matchesFilter = rate < 50;
			return matchesSearch && matchesFilter;
		});
	}, [sorted, searchQuery, statusFilter]);

	const summary = useMemo<IndexerSummary | null>(() => {
		if (!stats || stats.length === 0) return null;
		const totalImports = stats.reduce((s, x) => s + x.total_imports, 0);
		const totalSuccess = stats.reduce((s, x) => s + x.success_count, 0);
		const totalFailed = stats.reduce((s, x) => s + x.failed_count, 0);
		const overallRate = totalImports > 0 ? (totalSuccess / totalImports) * 100 : 0;
		return { totalImports, totalSuccess, totalFailed, overallRate };
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
				<div className="card overflow-hidden border border-base-200/40 bg-base-100/20 shadow backdrop-blur-md">
					<div className="space-y-3 p-4">
						{[...Array(5)].map((_, i) => (
							<div key={i} className="flex items-center gap-4">
								<div className="h-8 w-40 animate-pulse rounded bg-base-300/40" />
								<div className="h-4 w-12 animate-pulse rounded bg-base-300/30" />
								<div className="h-4 w-20 animate-pulse rounded bg-base-300/30" />
								<div className="h-2.5 w-24 animate-pulse rounded-full bg-base-300/40" />
								<div className="ml-auto h-4 w-10 animate-pulse rounded bg-base-300/30" />
								<div className="h-4 w-10 animate-pulse rounded bg-base-300/30" />
								<div className="h-4 w-10 animate-pulse rounded bg-base-300/30" />
							</div>
						))}
					</div>
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
		<div className="card overflow-hidden border border-base-200/40 bg-base-100/20 shadow-2xl backdrop-blur-md">
			<div className="card-body gap-0 p-0">
				<div className="flex flex-col gap-4 border-base-200/50 border-b bg-base-200/30 p-4 sm:flex-row sm:items-center sm:justify-between">
					<h2 className="flex items-center gap-2 font-semibold text-xl">
						<Radio className="h-6 w-6" aria-hidden="true" />
						Indexer Health
					</h2>
					<div className="flex flex-wrap items-center gap-2">
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
							className="btn btn-warning btn-sm gap-1.5 transition-all duration-200"
							onClick={() => setShowPruneModal(true)}
							disabled={!hasStats}
							aria-label="Prune indexer statistics history"
						>
							<Trash2 className="h-4 w-4" aria-hidden="true" />
							Prune Stats
						</button>
					</div>
				</div>

				{summary && (
					<div className="border-base-200/50 border-b p-4">
						<IndexerHealthSummary stats={stats ?? []} summary={summary} />
					</div>
				)}

				{hasStats && (
					<div className="border-base-200/50 border-b p-4">
						<IndexerHealthFilters
							searchQuery={searchQuery}
							onSearchChange={setSearchQuery}
							statusFilter={statusFilter}
							onStatusFilterChange={setStatusFilter}
							filteredCount={filtered.length}
						/>
					</div>
				)}

				{!hasStats ? (
					<div className="hero py-16">
						<div className="hero-content text-center">
							<div className="max-w-md space-y-4">
								<div className="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl border border-primary/20 bg-primary/5 text-primary shadow-sm">
									<BarChart3 className="h-8 w-8" aria-hidden="true" />
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
					<div className="hero py-12 text-center">
						<div className="max-w-xs space-y-2">
							<h3 className="font-bold text-base text-base-content/70">No Indexers Found</h3>
							<p className="text-base-content/40 text-xs">
								Try adjusting your fuzzy search or status filter options.
							</p>
						</div>
					</div>
				) : (
					<IndexerHealthTable
						items={filtered}
						sortKey={sortKey}
						sortAsc={sortAsc}
						onSort={handleSort}
						onDelete={handleDeleteIndexer}
					/>
				)}
			</div>

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
