// Configuration types that match the backend API structure

// Base configuration response from API
export interface ConfigResponse {
	webdav: WebDAVConfig;
	api: APIConfig;
	auth: AuthConfig;
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
	mount_path: string;
	api_key?: string;
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

// Authentication configuration
export interface AuthConfig {
	login_required: boolean;
}

// Database configuration
export interface DatabaseConfig {
	path: string;
}

// Metadata configuration
export interface MetadataConfig {
	root_path: string;
	delete_source_nzb_on_removal?: boolean;
}

// Streaming configuration
export interface StreamingConfig {
	max_download_workers: number;
	max_cache_size_mb: number;
}

// Health configuration
export interface HealthConfig {
	enabled: boolean;
	library_dir?: string;
	cleanup_orphaned_files?: boolean; // Clean up orphaned library files and metadata
	check_interval_seconds?: number; // Interval in seconds (optional)
	max_connections_for_health_checks?: number;
	max_concurrent_jobs?: number; // Max concurrent health check jobs
	segment_sample_percentage?: number; // Percentage of segments to check (1-100)
	library_sync_interval_minutes?: number; // Library sync interval in minutes (optional)
	check_all_segments?: boolean; // Whether to check all segments or use sampling
}

// Library sync types
export interface LibrarySyncProgress {
	total_files: number;
	processed_files: number;
	start_time: string;
}

export interface LibrarySyncResult {
	files_added: number;
	files_deleted: number;
	metadata_deleted: number;
	duration: string;
	completed_at: string;
}

export interface LibrarySyncStatus {
	is_running: boolean;
	progress?: LibrarySyncProgress;
	last_sync_result?: LibrarySyncResult;
}

// Dry run result for library sync
export interface DryRunSyncResult {
	orphaned_metadata_count: number; // Number of orphaned metadata files
	orphaned_library_files: number; // Number of orphaned library files (symlinks/STRM)
	database_records_to_clean: number; // Number of database records to clean
	would_cleanup: boolean; // Whether cleanup would occur based on config
}

// RClone configuration (sanitized)
export interface RCloneConfig {
	// Encryption
	password_set: boolean;
	salt_set: boolean;

	// RC (Remote Control) Configuration
	rc_enabled: boolean;
	rc_url: string;
	rc_port: number;
	rc_user: string;
	rc_pass_set: boolean;
	rc_options: Record<string, string>;

	// Mount Configuration
	mount_enabled: boolean;
	mount_options: Record<string, string>;

	// Mount-Specific Settings
	allow_other: boolean;
	allow_non_empty: boolean;
	read_only: boolean;
	timeout: string;
	syslog: boolean;

	// System and filesystem options
	log_level: string;
	uid: number;
	gid: number;
	umask: string;
	buffer_size: string;
	attr_timeout: string;
	transfers: number;

	// VFS Cache Settings
	cache_dir: string;
	vfs_cache_mode: string;
	vfs_cache_max_size: string;
	vfs_cache_max_age: string;
	read_chunk_size: string;
	read_chunk_size_limit: string;
	vfs_read_ahead: string;
	dir_cache_time: string;
	vfs_cache_min_free_space: string;
	vfs_disk_space_total: string;
	vfs_read_chunk_streams: number;

	// Advanced Settings
	no_mod_time: boolean;
	no_checksum: boolean;
	async_read: boolean;
	vfs_fast_fingerprint: boolean;
	use_mmap: boolean;
}

// Import strategy type
export type ImportStrategy = "NONE" | "SYMLINK" | "STRM";

// Import configuration
export interface ImportConfig {
	max_processor_workers: number;
	queue_processing_interval_seconds: number; // Interval in seconds for queue processing
	allowed_file_extensions: string[];
	max_import_connections: number;
	import_cache_size_mb: number;
	segment_sample_percentage: number; // Percentage of segments to check (1-100)
	import_strategy: ImportStrategy;
	import_dir?: string;
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
	last_speed_test_mbps?: number;
	last_speed_test_time?: string;
}

// SABnzbd configuration
export interface SABnzbdConfig {
	enabled: boolean;
	complete_dir: string;
	categories: SABnzbdCategory[];
	fallback_host?: string;
	fallback_api_key?: string; // Obfuscated when returned from API
	fallback_api_key_set?: boolean; // For display purposes only
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
	auth?: AuthUpdateRequest;
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
	mount_path?: string;
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

// Auth update request
export interface AuthUpdateRequest {
	login_required?: boolean;
}

// Database update request
export interface DatabaseUpdateRequest {
	path?: string;
}

// Metadata update request
export interface MetadataUpdateRequest {
	root_path?: string;
	delete_source_nzb_on_removal?: boolean;
}

// Streaming update request
export interface StreamingUpdateRequest {
	max_download_workers?: number;
	max_cache_size_mb?: number;
}

// Health update request
export interface HealthUpdateRequest {
	auto_repair_enabled?: boolean;
	check_interval_seconds?: number; // Interval in seconds (optional)
	max_connections_for_health_checks?: number;
	max_concurrent_jobs?: number; // Max concurrent health check jobs
	library_sync_interval_minutes?: number; // Library sync interval in minutes (optional)
	check_all_segments?: boolean; // Whether to check all segments or use sampling
}

// RClone update request
export interface RCloneUpdateRequest {
	password?: string;
	salt?: string;
	rc_enabled?: boolean;
	rc_url?: string;
	rc_port?: number;
	rc_user?: string;
	rc_pass?: string;
	rc_options?: Record<string, string>;
	mount_enabled?: boolean;
	mount_options?: Record<string, string>;

	// Mount-Specific Settings
	allow_other?: boolean;
	allow_non_empty?: boolean;
	read_only?: boolean;
	timeout?: string;
	syslog?: boolean;

	// System and filesystem options
	log_level?: string;
	uid?: number;
	gid?: number;
	umask?: string;
	buffer_size?: string;
	attr_timeout?: string;
	transfers?: number;

	// VFS Cache Settings
	cache_dir?: string;
	vfs_cache_mode?: string;
	vfs_cache_max_size?: string;
	vfs_cache_max_age?: string;
	read_chunk_size?: string;
	read_chunk_size_limit?: string;
	vfs_read_ahead?: string;
	dir_cache_time?: string;
	vfs_cache_min_free_space?: string;
	vfs_disk_space_total?: string;
	vfs_read_chunk_streams?: number;

	// Advanced Settings
	no_mod_time?: boolean;
	no_checksum?: boolean;
	async_read?: boolean;
	vfs_fast_fingerprint?: boolean;
	use_mmap?: boolean;
}

// Import update request
export interface ImportUpdateRequest {
	max_processor_workers?: number;
	queue_processing_interval_seconds?: number; // Interval in seconds for queue processing
	allowed_file_extensions?: string[];
	import_strategy?: ImportStrategy;
	import_dir?: string;
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
	complete_dir?: string;
	categories?: SABnzbdCategory[];
	fallback_host?: string;
	fallback_api_key?: string;
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
	| "auth"
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
	queue_processing_interval_seconds: number; // Interval in seconds for queue processing
	import_strategy: ImportStrategy;
	import_dir: string;
}

export interface MetadataFormData {
	root_path: string;
	delete_source_nzb_on_removal?: boolean;
}

export interface StreamingFormData {
	max_download_workers: number;
	max_cache_size_mb: number;
}

export interface RCloneFormData {
	password: string;
	salt: string;
	rc_enabled: boolean;
	rc_url: string;
	rc_port: number;
	rc_user: string;
	rc_pass: string;
	rc_options: Record<string, string>;
	mount_enabled: boolean;
	mount_options: Record<string, string>;

	// Mount-Specific Settings
	allow_other: boolean;
	allow_non_empty: boolean;
	read_only: boolean;
	timeout: string;
	syslog: boolean;

	// System and filesystem options
	log_level: string;
	uid: number;
	gid: number;
	umask: string;
	buffer_size: string;
	attr_timeout: string;
	transfers: number;

	// VFS Cache Settings
	cache_dir: string;
	vfs_cache_mode: string;
	vfs_cache_max_size: string;
	vfs_cache_max_age: string;
	read_chunk_size: string;
	read_chunk_size_limit: string;
	vfs_read_ahead: string;
	dir_cache_time: string;
	vfs_cache_min_free_space: string;
	vfs_disk_space_total: string;
	vfs_read_chunk_streams: number;

	// Advanced Settings
	no_mod_time: boolean;
	no_checksum: boolean;
	async_read: boolean;
	vfs_fast_fingerprint: boolean;
	use_mmap: boolean;
}

export interface RCloneRCFormData {
	rc_enabled: boolean;
	rc_url: string;
	rc_port: number;
	rc_user: string;
	rc_pass: string;
	rc_options: Record<string, string>;
}

export interface RCloneMountFormData {
	mount_enabled: boolean;
	mount_options: Record<string, string>;

	// Mount-Specific Settings
	allow_other: boolean;
	allow_non_empty: boolean;
	read_only: boolean;
	timeout: string;
	syslog: boolean;

	// System and filesystem options
	log_level: string;
	uid: number;
	gid: number;
	umask: string;
	buffer_size: string;
	attr_timeout: string;
	transfers: number;

	// VFS Cache Settings
	cache_dir: string;
	vfs_cache_mode: string;
	vfs_cache_max_size: string;
	vfs_cache_max_age: string;
	read_chunk_size: string;
	read_chunk_size_limit: string;
	vfs_read_ahead: string;
	dir_cache_time: string;
	vfs_cache_min_free_space: string;
	vfs_disk_space_total: string;
	vfs_read_chunk_streams: number;

	// Advanced Settings
	no_mod_time: boolean;
	no_checksum: boolean;
	async_read: boolean;
	vfs_fast_fingerprint: boolean;
	use_mmap: boolean;
}

export interface MountStatus {
	mounted: boolean;
	mount_point: string;
	error?: string;
	started_at?: string;
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

export interface SABnzbdFormData {
	enabled: boolean;
	complete_dir: string;
	categories: SABnzbdCategory[];
	fallback_host: string;
	fallback_api_key: string;
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
	auth: {
		title: "Authentication",
		description: "User authentication and login settings",
		icon: "Shield",
		canEdit: true,
	},
	rclone: {
		title: "Mount & RClone",
		description: "RClone mount and VFS settings",
		icon: "HardDrive",
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
