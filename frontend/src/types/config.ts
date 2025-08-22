// Configuration types that match the backend API structure

// Base configuration response from API
export interface ConfigResponse {
	webdav: WebDAVConfig;
	api: APIConfig;
	database: DatabaseConfig;
	metadata: MetadataConfig;
	watch_path: string;
	rclone: RCloneConfig;
	workers: WorkersConfig;
	providers: ProviderConfig[];
	debug: boolean;
}

// WebDAV server configuration
export interface WebDAVConfig {
	port: number;
	user: string;
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
	max_range_size: number;
	streaming_chunk_size: number;
}

// RClone configuration (sanitized)
export interface RCloneConfig {
	password_set: boolean;
	salt_set: boolean;
}

// Workers configuration
export interface WorkersConfig {
	download: number;
	processor: number;
}

// NNTP Provider configuration (sanitized)
export interface ProviderConfig {
	name: string;
	host: string;
	port: number;
	username: string;
	max_connections: number;
	tls: boolean;
	insecure_tls: boolean;
	password_set: boolean;
}

// Configuration update request types
export interface ConfigUpdateRequest {
	webdav?: WebDAVUpdateRequest;
	api?: APIUpdateRequest;
	database?: DatabaseUpdateRequest;
	metadata?: MetadataUpdateRequest;
	watch_path?: string;
	rclone?: RCloneUpdateRequest;
	workers?: WorkersUpdateRequest;
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
	max_range_size?: number;
	streaming_chunk_size?: number;
}

// RClone update request
export interface RCloneUpdateRequest {
	password?: string;
	salt?: string;
}

// Workers update request
export interface WorkersUpdateRequest {
	download?: number;
	processor?: number;
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
	| "rclone"
	| "workers"
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

export interface WorkersFormData {
	download: number;
	processor: number;
}

export interface MetadataFormData {
	root_path: string;
	max_range_size: number;
	streaming_chunk_size: number;
}

export interface RCloneFormData {
	password: string;
	salt: string;
}

export interface ProviderFormData {
	name: string;
	host: string;
	port: number;
	username: string;
	password: string;
	max_connections: number;
	tls: boolean;
	insecure_tls: boolean;
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
	requiresRestart?: boolean;
}

// Configuration sections metadata
export const CONFIG_SECTIONS: Record<
	ConfigSection | "system",
	ConfigSectionInfo
> = {
	webdav: {
		title: "WebDAV Server",
		description: "WebDAV server settings for file access",
		icon: "Globe",
		canEdit: true,
		requiresRestart: true, // Port changes require restart
	},
	metadata: {
		title: "Metadata",
		description: "File metadata storage and processing settings",
		icon: "Folder",
		canEdit: true,
	},
	rclone: {
		title: "RClone Encryption",
		description: "RClone encryption password and salt configuration",
		icon: "Shield",
		canEdit: true,
	},
	workers: {
		title: "Worker Processes",
		description: "Download and processor worker configuration",
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
