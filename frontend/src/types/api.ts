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
	RETRYING: "retrying",
} as const;

export type QueueStatus = (typeof QueueStatus)[keyof typeof QueueStatus];

export interface QueueItem {
	id: number;
	nzb_path: string;
	target_path: string;
	category?: string;
	watch_root?: string;
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
}

export interface QueueStats {
	total_queued: number;
	total_processing: number;
	total_completed: number;
	total_failed: number;
	avg_processing_time_ms: number;
	last_updated: string;
}

export interface QueueRetryRequest {
	reset_retry_count?: boolean;
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

// Health types
export const HealthStatus = {
	PENDING: "pending",
	CHECKING: "checking",
	HEALTHY: "healthy",
	PARTIAL: "partial",
	CORRUPTED: "corrupted",
} as const;

export type HealthStatus = (typeof HealthStatus)[keyof typeof HealthStatus];

export interface FileHealth {
	id: number;
	file_path: string;
	status: HealthStatus;
	retry_count: number;
	source_nzb_path?: string;
	error_message?: string;
	last_checked?: string;
	created_at: string;
	updated_at: string;
}

export interface HealthStats {
	total: number;
	pending: number;
	healthy: number;
	partial: number;
	corrupted: number;
}

export interface HealthRetryRequest {
	reset_status?: boolean;
}

export interface HealthCleanupRequest {
	older_than?: string;
	status?: HealthStatus;
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
	status: "healthy" | "partial" | "corrupted" | "unspecified";
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

// Health Worker types
export interface HealthCheckRequest {
	file_path: string;
	source_nzb_path: string;
	priority?: boolean;
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

// Pool Metrics types
export interface PoolMetrics {
	active_connections: number;
	total_bytes_downloaded: number;
	download_speed_bytes_per_sec: number;
	error_rate_percent: number;
	current_memory_usage: number;
	total_connections: number;
	command_success_rate_percent: number;
	acquire_wait_time_ms: number;
	last_updated: string;
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
