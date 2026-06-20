export interface IndexerStat {
	indexer: string;
	total_imports: number;
	success_count: number;
	failed_count: number;
	last_24h_count: number;
	success_rate: number;
	last_seen_at: string;
}

export interface IndexerSummary {
	totalImports: number;
	totalSuccess: number;
	totalFailed: number;
	overallRate: number;
}

export type SortKey = "name" | "last_24h" | "last_seen" | "health" | "success" | "failed" | "total";

export const SORT_KEYS: SortKey[] = [
	"name",
	"last_24h",
	"last_seen",
	"health",
	"success",
	"failed",
	"total",
];
