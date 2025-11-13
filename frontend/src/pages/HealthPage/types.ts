export interface CleanupConfig {
	older_than: string;
	delete_files: boolean;
}

export interface HealthCheckForm {
	file_path: string;
	source_nzb_path: string;
	priority: boolean;
}

export type SortBy = "file_path" | "created_at" | "status";
export type SortOrder = "asc" | "desc";
