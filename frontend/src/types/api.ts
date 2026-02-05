// Base API Response types
export interface APIResponse<T = unknown> {
	success: boolean;
	data?: T;
	error?: string;
	meta?: APIMeta;
}

export interface APIMeta {
	count: number;
	limit: number;
	offset: number;
	total?: number;
}

// Queue types
export const QueueStatus = {
	PENDING: "pending",
	PROCESSING: "processing",
	COMPLETED: "completed",
	FAILED: "failed",
} as const;

export type QueueStatus = (typeof QueueStatus)[keyof typeof QueueStatus];

export interface QueueItem {
	id: number;
	nzb_path: string;
	target_path: string;
	category?: string;
	relative_path?: string;
	priority: number;
	status: QueueStatus;
	created_at: string;
	updated_at: string;
	started_at?: string;
	completed_at?: string;
	retry_count: number;
	max_retries: number;
	error_message?: string;
	batch_id?: string;
	metadata?: string;
	file_size?: number;
	percentage?: number; // Progress percentage (0-100), only present for items being processed
}

export interface ProgressUpdate {
	id: number;
	percentage: number;
}

export interface QueueStats {
	total_queued: number;
	total_processing: number;
	total_completed: number;
	total_failed: number;
	avg_processing_time_ms: number;
	last_updated: string;
}

// NZBLNK upload types
export interface NZBLnkResult {
	link: string;
	success: boolean;
	queue_id?: number;
	title?: string;
	error_message?: string;
}

export interface UploadNZBLnkResponse {
	results: NZBLnkResult[];
	success_count: number;
	failed_count: number;
}

// Manual Scan types
export const ScanStatus = {
	IDLE: "idle",
	SCANNING: "scanning",
	CANCELING: "canceling",
} as const;

export type ScanStatus = (typeof ScanStatus)[keyof typeof ScanStatus];

export interface ManualScanRequest {
	path: string;
}

export interface ScanStatusResponse {
	status: ScanStatus;
	path?: string;
	start_time?: string;
	files_found: number;
	files_added: number;
	current_file?: string;
	last_error?: string;
}

// Import Job types
export const ImportJobStatus = {
	IDLE: "idle",
	RUNNING: "running",
	CANCELING: "canceling",
	COMPLETED: "completed",
} as const;

export type ImportJobStatus = (typeof ImportJobStatus)[keyof typeof ImportJobStatus];

export interface ImportStatusResponse {
	status: ImportJobStatus;
	total: number;
	added: number;
	failed: number;
	skipped?: number;
	last_error?: string;
}

// Health types
export const HealthStatus = {
	PENDING: "pending",
	CHECKING: "checking",
	HEALTHY: "healthy",
	CORRUPTED: "corrupted",
	REPAIR_TRIGGERED: "repair_triggered",
} as const;

export type HealthStatus = (typeof HealthStatus)[keyof typeof HealthStatus];

export const HealthPriority = {
	Normal: 0,
	High: 1,
	Next: 2,
} as const;

export type HealthPriority = (typeof HealthPriority)[keyof typeof HealthPriority];

export interface FileHealth {
	id: number;
	file_path: string;
	status: HealthStatus;
	last_checked: string;
	last_error?: string;
	retry_count: number;
	max_retries: number;
	source_nzb_path?: string;
	library_path?: string;
	error_details?: string;
	repair_retry_count: number;
	max_repair_retries: number;
	created_at: string;
	updated_at: string;
	scheduled_check_at?: string;
	priority: HealthPriority;
}

export interface HealthStats {
	total: number;
	pending: number;
	healthy: number;
	corrupted: number;
	repair_triggered: number;
	checking: number;
}

export interface HealthRetryRequest {
	reset_status?: boolean;
}

export interface HealthRepairRequest {
	reset_repair_retry_count?: boolean;
}

export interface HealthCleanupRequest {
	older_than?: string;
	status?: HealthStatus;
	delete_files?: boolean;
}

export interface HealthCleanupResponse {
	records_deleted: number;
	files_deleted?: number;
	older_than: string;
	status_filter?: HealthStatus;
	file_deletion_errors?: string[];
	warning?: string;
}

// System types
export interface SystemInfo {
	start_time: string;
	uptime: string;
	go_version: string;
}

export interface ComponentHealth {
	status: "healthy" | "unhealthy" | "degraded";
	message: string;
	details?: string;
}

export interface SystemHealth {
	status: "healthy" | "unhealthy" | "degraded";
	timestamp: string;
	components: Record<string, ComponentHealth>;
}

export interface SystemCleanupRequest {
	queue_older_than?: string;
	health_older_than?: string;
	health_status?: HealthStatus;
}

// File metadata types
export interface SegmentInfo {
	message_id: string;
	segment_size: number;
	start_offset: number;
	end_offset: number;
	available: boolean;
}

export interface FileMetadata {
	file_size: number;
	source_nzb_path: string;
	status: "corrupted" | "unspecified";
	segment_count: number;
	available_segments?: number;
	encryption: "none" | "rclone";
	created_at: string;
	modified_at: string;
	password_protected: boolean;
	segments: SegmentInfo[];
}

// Filter and pagination types
export interface PaginationParams {
	limit?: number;
	offset?: number;
}

export interface QueueFilters extends PaginationParams {
	status?: QueueStatus;
	since?: string;
}

export interface HealthFilters extends PaginationParams {
	status?: HealthStatus;
	since?: string;
}

// Authentication types
export interface User {
	id: string;
	email?: string;
	name: string;
	avatar_url?: string;
	provider: string;
	api_key?: string;
	is_admin: boolean;
	last_login?: string;
}

export interface AuthResponse {
	user?: User;
	redirect_url?: string;
	message?: string;
}

export interface LoginRequest {
	provider: string;
}

export interface UserAdminUpdateRequest {
	is_admin: boolean;
}

export interface ManualImportRequest {
	file_path: string;
	relative_path?: string;
}

export interface ManualImportResponse {
	queue_id: number;
	message: string;
}

// Health Worker types
export interface HealthCheckRequest {
	file_path: string;
	source_nzb_path: string;
	priority?: HealthPriority;
}

export interface HealthWorkerStatus {
	status: string;
	total_runs_completed: number;
	total_files_checked: number;
	total_files_recovered: number;
	total_files_corrupted: number;
	current_run_files_checked: number;
	pending_manual_checks: number;
	error_count: number;
	current_run_start_time?: string;
	last_run_time?: string;
	next_run_time?: string;
	last_error?: string;
}

// Library Sync types
export interface LibrarySyncProgress {
	total_files: number;
	processed_files: number;
	start_time: string;
}

export interface LibrarySyncResult {
	files_added: number;
	files_deleted: number;
	duration: number;
	completed_at: string;
}

export interface LibrarySyncStatus {
	is_running: boolean;
	progress?: LibrarySyncProgress;
	last_sync_result?: LibrarySyncResult;
}

// Pool Metrics types
export interface ProviderStatus {
	id: string;
	host: string;
	username: string;
	used_connections: number;
	max_connections: number;
	state: string;
	error_count: number;
	last_connection_attempt: string;
	last_successful_connect: string;
	failure_reason: string;
	last_speed_test_mbps: number;
	last_speed_test_time?: string;
}

export interface ActiveStream {
	id: string;
	file_path: string;
	started_at: string;
	source: string;
	user_name?: string;
	client_ip?: string;
	user_agent?: string;
	bytes_sent: number;
	current_offset: number;
	bytes_per_second: number;
	speed_avg: number;
	total_size: number;
	eta: number;
	status: string;
	total_connections: number;
	buffered_offset: number;
}

export interface PoolMetrics {
	bytes_downloaded: number;
	bytes_uploaded: number;
	articles_downloaded: number;
	articles_posted: number;
	total_errors: number;
	provider_errors: Record<string, number>;
	download_speed_bytes_per_sec: number;
	max_download_speed_bytes_per_sec: number;
	upload_speed_bytes_per_sec: number;
	timestamp: string;
	providers: ProviderStatus[];
}

// SABnzbd API response types
export interface SABnzbdAddResponse {
	status: boolean;
	nzo_ids: string[];
}

export interface SABnzbdResponse {
	status: boolean;
	error?: string;
}

// System Browse types
export interface FileEntry {
	name: string;
	path: string;
	is_dir: boolean;
	size: number;
	mod_time: string;
}

export interface SystemBrowseResponse {
	current_path: string;
	parent_path: string;
	files: FileEntry[];
}

// FUSE types
export interface FuseStatus {
	status: "stopped" | "starting" | "running" | "error";
	path: string;
}
