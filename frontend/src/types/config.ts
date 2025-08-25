// Configuration types that match the backend API structure

// Base configuration response from API
export interface ConfigResponse {
	webdav: WebDAVConfig;
	api: APIConfig;
	database: DatabaseConfig;
	metadata: MetadataConfig;
	streaming: StreamingConfig;
	watch_path: string;
	rclone: RCloneConfig;
	import: ImportConfig;
	providers: ProviderConfig[];
	debug: boolean;
}

// WebDAV server configuration
export interface WebDAVConfig {
	port: number;
	user: string;
	password: string;
	debug: boolean;
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

// RClone configuration (sanitized)
export interface RCloneConfig {
	password_set: boolean;
	salt_set: boolean;
}

// Import configuration
export interface ImportConfig {
	max_processor_workers: number;
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
}

// Configuration update request types
export interface ConfigUpdateRequest {
	webdav?: WebDAVUpdateRequest;
	api?: APIUpdateRequest;
	database?: DatabaseUpdateRequest;
	metadata?: MetadataUpdateRequest;
	streaming?: StreamingUpdateRequest;
	watch_path?: string;
	rclone?: RCloneUpdateRequest;
	import?: ImportUpdateRequest;
	providers?: ProviderUpdateRequest[];
	debug?: boolean;
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

// RClone update request
export interface RCloneUpdateRequest {
	password?: string;
	salt?: string;
}

// Import update request
export interface ImportUpdateRequest {
	max_processor_workers?: number;
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
	| "rclone"
	| "import"
	| "providers";

// Form data interfaces for UI components
export interface WebDAVFormData {
	port: number;
	user: string;
	password: string;
	debug: boolean;
}

export interface APIFormData {
	prefix: string;
}

export interface ImportFormData {
	max_processor_workers: number;
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
}

export interface SystemFormData {
	watch_path: string;
	debug: boolean;
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
	latency_ms?: number;
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
}

export interface ProviderReorderRequest {
	provider_ids: string[];
}

export const CONFIG_SECTIONS: Record<
	ConfigSection | "system",
	ConfigSectionInfo
> = {
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
	rclone: {
		title: "RClone Encryption",
		description: "RClone encryption password and salt configuration",
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
	system: {
		title: "System Paths",
		description: "Mount paths and system directories",
		icon: "HardDrive",
		canEdit: true,
	},
};
