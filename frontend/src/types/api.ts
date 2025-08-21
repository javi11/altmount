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
	status: QueueStatus;
	retry_count: number;
	error_message?: string;
	created_at: string;
	updated_at: string;
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

// Health types
export const HealthStatus = {
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
	source_nzb_path: string;
	error_message?: string;
	last_check?: string;
	created_at: string;
	updated_at: string;
}

export interface HealthStats {
	total: number;
	healthy: number;
	partial: number;
	corrupted: number;
	last_check?: string;
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
