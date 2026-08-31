export interface CleanupConfig {
	older_than: string;
	delete_files: boolean;
}

export type SortBy =
	| "file_path"
	| "created_at"
	| "status"
	| "priority"
	| "last_checked"
	| "scheduled_check_at";
export type SortOrder = "asc" | "desc";
