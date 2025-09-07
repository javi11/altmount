// Configuration types that match the backend API structure

// Base configuration response from API
export interface ConfigResponse {
	webdav: WebDAVConfig;
	api: APIConfig;
	database: DatabaseConfig;
	metadata: MetadataConfig;
	streaming: StreamingConfig;
	health: HealthConfig;
	rclone: RCloneConfig;
	import: ImportConfig;
	log: LogConfig;
	sabnzbd: SABnzbdConfig;
	arrs: ArrsConfig;
	providers: ProviderConfig[];
	log_level: string;
}

// WebDAV server configuration
export interface WebDAVConfig {
	port: number;
	user: string;
	password: string;
}

// API server configuration
export interface APIConfig {
	prefix: string;
}

// Database configuration
export interface DatabaseConfig {
	path: string;
}

// Metadata configuration
export interface MetadataConfig {
	root_path: string;
}

// Streaming configuration
export interface StreamingConfig {
	max_range_size: number;
	streaming_chunk_size: number;
	max_download_workers: number;
}

// Health configuration
export interface HealthConfig {
	enabled: boolean;
	auto_repair_enabled: boolean;
	check_interval?: number; // Duration in nanoseconds (optional)
	max_concurrent_jobs?: number;
	max_retries?: number;
	max_segment_connections?: number;
	check_all_segments?: boolean;
}

// RClone configuration (sanitized)
export interface RCloneConfig {
	password_set: boolean;
	salt_set: boolean;
	vfs_enabled: boolean;
	vfs_url: string;
	vfs_user: string;
	vfs_pass_set: boolean;
}

// Import configuration
export interface ImportConfig {
	max_processor_workers: number;
	queue_processing_interval: number; // Interval in seconds for queue processing
}

// Log configuration
export interface LogConfig {
	file: string;
	level: string;
	max_size: number;
	max_age: number;
	max_backups: number;
	compress: boolean;
}

// NNTP Provider configuration (sanitized)
export interface ProviderConfig {
	id: string;
	host: string;
	port: number;
	username: string;
	max_connections: number;
	tls: boolean;
	insecure_tls: boolean;
	password_set: boolean;
	enabled: boolean;
	is_backup_provider: boolean;
}

// SABnzbd configuration
export interface SABnzbdConfig {
	enabled: boolean;
	mount_dir: string;
	categories: SABnzbdCategory[];
}

// SABnzbd category configuration
export interface SABnzbdCategory {
	name: string;
	order: number;
	priority: number;
	dir: string;
}

// Configuration update request types
export interface ConfigUpdateRequest {
	webdav?: WebDAVUpdateRequest;
	api?: APIUpdateRequest;
	database?: DatabaseUpdateRequest;
	metadata?: MetadataUpdateRequest;
	streaming?: StreamingUpdateRequest;
	health?: HealthUpdateRequest;
	rclone?: RCloneUpdateRequest;
	import?: ImportUpdateRequest;
	log?: LogUpdateRequest;
	sabnzbd?: SABnzbdUpdateRequest;
	arrs?: ArrsConfig;
	providers?: ProviderUpdateRequest[];
	log_level?: string;
}

// WebDAV update request
export interface WebDAVUpdateRequest {
	port?: number;
	user?: string;
	password?: string;
	debug?: boolean;
}

// API update request
export interface APIUpdateRequest {
	prefix?: string;
}

// Database update request
export interface DatabaseUpdateRequest {
	path?: string;
}

// Metadata update request
export interface MetadataUpdateRequest {
	root_path?: string;
}

// Streaming update request
export interface StreamingUpdateRequest {
	max_range_size?: number;
	streaming_chunk_size?: number;
	max_download_workers?: number;
}

// Health update request
export interface HealthUpdateRequest {
	enabled?: boolean;
	auto_repair_enabled?: boolean;
	check_interval?: number; // Duration in nanoseconds (optional)
	max_concurrent_jobs?: number;
	max_retries?: number;
	max_segment_connections?: number;
	check_all_segments?: boolean;
}

// RClone update request
export interface RCloneUpdateRequest {
	password?: string;
	salt?: string;
	vfs_enabled?: boolean;
	vfs_url?: string;
	vfs_user?: string;
	vfs_pass?: string;
}

// Import update request
export interface ImportUpdateRequest {
	max_processor_workers?: number;
	queue_processing_interval?: number; // Interval in seconds for queue processing
}

// Log update request
export interface LogUpdateRequest {
	file?: string;
	level?: string;
	max_size?: number;
	max_age?: number;
	max_backups?: number;
	compress?: boolean;
}

// Provider update request
export interface ProviderUpdateRequest {
	name?: string;
	host?: string;
	port?: number;
	username?: string;
	password?: string;
	max_connections?: number;
	tls?: boolean;
	insecure_tls?: boolean;
	enabled?: boolean;
	is_backup_provider?: boolean;
}

// SABnzbd update request
export interface SABnzbdUpdateRequest {
	enabled?: boolean;
	mount_dir?: string;
	categories?: SABnzbdCategory[];
}

// Configuration validation request
export interface ConfigValidateRequest {
	config: unknown;
}

// Configuration validation response
export interface ConfigValidateResponse {
	valid: boolean;
	errors?: ConfigValidationError[];
}

// Configuration validation error
export interface ConfigValidationError {
	field: string;
	message: string;
}

// Configuration section names for PATCH requests
export type ConfigSection =
	| "webdav"
	| "metadata"
	| "streaming"
	| "health"
	| "import"
	| "providers"
	| "rclone"
	| "sabnzbd"
	| "arrs"
	| "system";

// Form data interfaces for UI components
export interface WebDAVFormData {
	port: number;
	user: string;
	password: string;
}

export interface APIFormData {
	prefix: string;
}

export interface ImportFormData {
	max_processor_workers: number;
	queue_processing_interval: number; // Interval in seconds for queue processing
}

export interface MetadataFormData {
	root_path: string;
}

export interface StreamingFormData {
	max_range_size: number;
	streaming_chunk_size: number;
	max_download_workers: number;
}

export interface RCloneFormData {
	password: string;
	salt: string;
	vfs_enabled: boolean;
	vfs_url: string;
	vfs_user: string;
	vfs_pass: string;
}

export interface RCloneVFSFormData {
	vfs_enabled: boolean;
	vfs_url: string;
	vfs_user: string;
	vfs_pass: string;
}

export interface ProviderFormData {
	host: string;
	port: number;
	username: string;
	password: string;
	max_connections: number;
	tls: boolean;
	insecure_tls: boolean;
	enabled: boolean;
	is_backup_provider: boolean;
}

export interface LogFormData {
	file: string;
	level: string;
	max_size: number;
	max_age: number;
	max_backups: number;
	compress: boolean;
}

export interface SystemFormData {
	log_level: string;
}

export interface SABnzbdFormData {
	enabled: boolean;
	mount_dir: string;
	categories: SABnzbdCategory[];
}

// Arrs configuration types
export type ArrsType = "radarr" | "sonarr";

// Sync status types
export type SyncStatus = "idle" | "running" | "cancelling" | "completed" | "failed";

export interface PathMappingConfig {
	from_path: string;
	to_path: string;
}

export interface ArrsInstanceConfig {
	name: string;
	url: string;
	api_key: string;
	enabled: boolean;
	sync_interval_hours: number;
}

// Database-backed arrs instance (includes real ID from database)
export interface ArrsInstance {
	id: number;
	name: string;
	type: ArrsType;
	url: string;
	api_key: string;
	enabled: boolean;
	sync_interval_hours: number;
	last_sync_at?: string;
	created_at: string;
	updated_at: string;
}

export interface ArrsConfig {
	enabled: boolean;
	max_workers: number;
	mount_path: string;
	radarr_instances: ArrsInstanceConfig[];
	sonarr_instances: ArrsInstanceConfig[];
}

// Sync status and progress types
export interface SyncProgressInfo {
	processed_count: number;
	error_count: number;
	total_items?: number;
	current_batch: string;
}

export interface SyncProgress {
	instance_id: number;
	status: SyncStatus;
	started_at: string;
	processed_count: number;
	error_count: number;
	total_items?: number;
	current_batch: string;
}

export interface SyncResult {
	instance_id: number;
	status: SyncStatus;
	started_at: string;
	completed_at: string;
	processed_count: number;
	error_count: number;
	error_message?: string;
}

export interface ArrsFormData {
	enabled: boolean;
	max_workers: number;
	mount_path: string;
	radarr_instances: ArrsInstanceConfig[];
	sonarr_instances: ArrsInstanceConfig[];
}

// Helper type for configuration sections
export interface ConfigSectionInfo {
	title: string;
	description: string;
	icon: string;
	canEdit: boolean;
}

// Configuration sections metadata
// Provider management types
export interface ProviderTestRequest {
	host: string;
	port: number;
	username: string;
	password: string;
	tls: boolean;
	insecure_tls: boolean;
}

export interface ProviderTestResponse {
	success: boolean;
	error_message?: string;
}

export interface ProviderCreateRequest {
	host: string;
	port: number;
	username: string;
	password: string;
	max_connections: number;
	tls: boolean;
	insecure_tls: boolean;
	enabled: boolean;
	is_backup_provider: boolean;
}

export interface ProviderReorderRequest {
	provider_ids: string[];
}

export const CONFIG_SECTIONS: Record<ConfigSection | "system", ConfigSectionInfo> = {
	webdav: {
		title: "WebDAV Server",
		description: "WebDAV server settings for file access",
		icon: "Globe",
		canEdit: true,
	},
	metadata: {
		title: "Metadata",
		description: "File metadata storage settings",
		icon: "Folder",
		canEdit: true,
	},
	streaming: {
		title: "Streaming & Downloads",
		description: "File streaming, chunking and download worker configuration",
		icon: "Download",
		canEdit: true,
	},
	health: {
		title: "Health Monitoring",
		description: "File health monitoring and automatic repair settings",
		icon: "Shield",
		canEdit: true,
	},
	import: {
		title: "Import Processing",
		description: "NZB import and processing worker configuration",
		icon: "Cog",
		canEdit: true,
	},
	providers: {
		title: "NNTP Providers",
		description: "Usenet provider configuration for downloads",
		icon: "Radio",
		canEdit: true,
	},
	rclone: {
		title: "RClone VFS",
		description: "RClone VFS notification settings for external mounts",
		icon: "HardDrive",
		canEdit: true,
	},
	sabnzbd: {
		title: "SABnzbd API",
		description: "SABnzbd-compatible API configuration for download clients",
		icon: "Download",
		canEdit: true,
	},
	arrs: {
		title: "Radarr/Sonarr Management",
		description:
			"Configure Radarr and Sonarr instances for movie and TV show file synchronization. This will allow to repair broken files by notifying the appropriate service.",
		icon: "Cog",
		canEdit: true,
	},
	system: {
		title: "System",
		description: "System settings",
		icon: "HardDrive",
		canEdit: true,
	},
};
