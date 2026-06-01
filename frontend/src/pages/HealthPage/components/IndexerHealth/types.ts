export interface IndexerStat {
	indexer: string;
	total_imports: number;
	success_count: number;
	failed_count: number;
	success_rate: number;
	last_seen_at: string;
}

export interface IndexerSummary {
	totalImports: number;
	totalSuccess: number;
	totalFailed: number;
	overallRate: number;
	best: IndexerStat;
	worst: IndexerStat;
}

export type SortKey = "health" | "total" | "name";
