import type {
	APIResponse,
	AuthResponse,
	FileHealth,
	FileMetadata,
	HealthCheckRequest,
	HealthCleanupRequest,
	HealthCleanupResponse,
	HealthStats,
	HealthWorkerStatus,
	LibrarySyncStatus,
	ManualScanRequest,
	PoolMetrics,
	QueueItem,
	QueueStats,
	SABnzbdAddResponse,
	ScanStatusResponse,
	User,
	UserAdminUpdateRequest,
} from "../types/api";
import type {
	ConfigResponse,
	ConfigSection,
	ConfigUpdateRequest,
	ConfigValidateRequest,
	ConfigValidateResponse,
	ProviderConfig,
	ProviderCreateRequest,
	ProviderReorderRequest,
	ProviderTestRequest,
	ProviderTestResponse,
	ProviderUpdateRequest,
} from "../types/config";

export class APIError extends Error {
	public status: number;
	public details: string;

	constructor(status: number, message: string, details: string) {
		super(message);
		this.status = status;
		this.name = "APIError";
		this.details = details;
	}
}

export class APIClient {
	private baseURL: string;

	constructor(baseURL = "/api") {
		this.baseURL = baseURL;
	}

	private async request<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
		const url = `${this.baseURL}${endpoint}`;

		const config: RequestInit = {
			credentials: "include", // Include cookies for Safari compatibility
			headers: {
				"Content-Type": "application/json",
				...options.headers,
			},
			...options,
		};

		try {
			const response = await fetch(url, config);

			if (!response.ok) {
				const errorData = await response.json();
				throw new APIError(
					response.status,
					errorData.message || `HTTP ${response.status}: ${response.statusText}`,
					errorData.details || "",
				);
			}

			const data: APIResponse<T> = await response.json();

			if (!data.success) {
				// Handle error in the success=false format
				throw new APIError(response.status, data.error || "API request failed", "");
			}

			return data.data as T;
		} catch (error) {
			if (error instanceof APIError) {
				throw error;
			}
			throw new APIError(0, error instanceof Error ? error.message : "Network error", "");
		}
	}

	private async requestWithMeta<T>(
		endpoint: string,
		options: RequestInit = {},
	): Promise<APIResponse<T>> {
		const url = `${this.baseURL}${endpoint}`;

		const config: RequestInit = {
			credentials: "include", // Include cookies for Safari compatibility
			headers: {
				"Content-Type": "application/json",
				...options.headers,
			},
			...options,
		};

		try {
			const response = await fetch(url, config);

			if (!response.ok) {
				// Try to parse error response
				try {
					const errorData = await response.json();
					throw new APIError(
						response.status,
						errorData.message || `HTTP ${response.status}: ${response.statusText}`,
						errorData.details || "",
					);
				} catch {
					// If parsing fails, use generic error
					throw new APIError(
						response.status,
						`HTTP ${response.status}: ${response.statusText}`,
						"",
					);
				}
			}

			const data: APIResponse<T> = await response.json();

			if (!data.success) {
				// Handle error in the success=false format
				throw new APIError(response.status, data.error || "API request failed", "");
			}

			return data;
		} catch (error) {
			if (error instanceof APIError) {
				throw error;
			}
			throw new APIError(0, error instanceof Error ? error.message : "Network error", "");
		}
	}

	// Queue endpoints
	async getQueue(params?: {
		limit?: number;
		offset?: number;
		status?: string;
		since?: string;
		search?: string;
	}) {
		const searchParams = new URLSearchParams();
		if (params?.limit) searchParams.set("limit", params.limit.toString());
		if (params?.offset) searchParams.set("offset", params.offset.toString());
		if (params?.status) searchParams.set("status", params.status);
		if (params?.since) searchParams.set("since", params.since);
		if (params?.search) searchParams.set("search", params.search);

		const query = searchParams.toString();
		return this.requestWithMeta<QueueItem[]>(`/queue${query ? `?${query}` : ""}`);
	}

	async getQueueItem(id: number) {
		return this.request<QueueItem>(`/queue/${id}`);
	}

	async deleteQueueItem(id: number) {
		return this.request<QueueItem>(`/queue/${id}`, { method: "DELETE" });
	}

	async deleteBulkQueueItems(ids: number[]) {
		return this.request<{ deleted_count: number; message: string }>("/queue/bulk", {
			method: "DELETE",
			headers: {
				"Content-Type": "application/json",
			},
			body: JSON.stringify({ ids }),
		});
	}

	async restartBulkQueueItems(ids: number[]) {
		return this.request<{ restarted_count: number; message: string }>("/queue/bulk/restart", {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
			},
			body: JSON.stringify({ ids }),
		});
	}

	async retryQueueItem(id: number) {
		return this.request<QueueItem>(`/queue/${id}/retry`, {
			method: "POST",
		});
	}

	async cancelQueueItem(id: number) {
		return this.request<{ message: string; id: number }>(`/queue/${id}/cancel`, {
			method: "POST",
		});
	}

	async cancelBulkQueueItems(ids: number[]) {
		return this.request<{
			cancelled_count: number;
			not_processing_count: number;
			not_found_count: number;
			results: Record<string, string>;
			message: string;
		}>("/queue/bulk/cancel", {
			method: "POST",
			body: JSON.stringify({ ids }),
		});
	}

	async getQueueStats() {
		return this.request<QueueStats>("/queue/stats");
	}

	async clearCompletedQueue(olderThan?: string) {
		const searchParams = new URLSearchParams();
		if (olderThan) searchParams.set("older_than", olderThan);

		const query = searchParams.toString();
		return this.request<QueueStats>(`/queue/completed${query ? `?${query}` : ""}`, {
			method: "DELETE",
		});
	}

	async clearFailedQueue(olderThan?: string) {
		const searchParams = new URLSearchParams();
		if (olderThan) searchParams.set("older_than", olderThan);

		const query = searchParams.toString();
		return this.request<QueueStats>(`/queue/failed${query ? `?${query}` : ""}`, {
			method: "DELETE",
		});
	}

	async clearPendingQueue(olderThan?: string) {
		const searchParams = new URLSearchParams();
		if (olderThan) searchParams.set("older_than", olderThan);

		const query = searchParams.toString();
		return this.request<QueueStats>(`/queue/pending${query ? `?${query}` : ""}`, {
			method: "DELETE",
		});
	}

	// Health endpoints
	async getHealth(params?: {
		limit?: number;
		offset?: number;
		status?: string;
		since?: string;
		search?: string;
		sort_by?: string;
		sort_order?: "asc" | "desc";
	}) {
		const searchParams = new URLSearchParams();
		if (params?.limit) searchParams.set("limit", params.limit.toString());
		if (params?.offset) searchParams.set("offset", params.offset.toString());
		if (params?.status) searchParams.set("status", params.status);
		if (params?.since) searchParams.set("since", params.since);
		if (params?.search) searchParams.set("search", params.search);
		if (params?.sort_by) searchParams.set("sort_by", params.sort_by);
		if (params?.sort_order) searchParams.set("sort_order", params.sort_order);

		const query = searchParams.toString();
		return this.requestWithMeta<FileHealth[]>(`/health${query ? `?${query}` : ""}`);
	}

	async getHealthItem(id: string) {
		return this.request<FileHealth>(`/health/${encodeURIComponent(id)}`);
	}

	async deleteHealthItem(id: number) {
		return this.request<FileHealth>(`/health/${id}`, {
			method: "DELETE",
		});
	}

	async deleteBulkHealthItems(filePaths: string[]) {
		return this.request<{
			message: string;
			deleted_count: number;
			file_paths: string[];
			deleted_at: string;
		}>("/health/bulk/delete", {
			method: "POST",
			body: JSON.stringify({ file_paths: filePaths }),
		});
	}

	async restartBulkHealthItems(filePaths: string[]) {
		return this.request<{
			message: string;
			restarted_count: number;
			file_paths: string[];
			restarted_at: string;
		}>("/health/bulk/restart", {
			method: "POST",
			body: JSON.stringify({ file_paths: filePaths }),
		});
	}

	async retryHealthItem(id: string, resetStatus?: boolean) {
		return this.request<FileHealth>(`/health/${encodeURIComponent(id)}/retry`, {
			method: "POST",
			body: JSON.stringify({ reset_status: resetStatus }),
		});
	}

	async repairHealthItem(id: number, resetRepairRetryCount?: boolean) {
		return this.request<FileHealth>(`/health/${id}/repair`, {
			method: "POST",
			body: JSON.stringify({ reset_repair_retry_count: resetRepairRetryCount }),
		});
	}

	async getCorruptedFiles(params?: { limit?: number; offset?: number }) {
		const searchParams = new URLSearchParams();
		if (params?.limit) searchParams.set("limit", params.limit.toString());
		if (params?.offset) searchParams.set("offset", params.offset.toString());

		const query = searchParams.toString();
		return this.request<FileHealth[]>(`/health/corrupted${query ? `?${query}` : ""}`);
	}

	async getHealthStats() {
		return this.request<HealthStats>("/health/stats");
	}

	async cleanupHealth(params?: HealthCleanupRequest) {
		return this.request<HealthCleanupResponse>("/health/cleanup", {
			method: "DELETE",
			body: JSON.stringify(params),
		});
	}

	async addHealthCheck(data: HealthCheckRequest) {
		return this.request<{ message: string }>("/health/check", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	async getHealthWorkerStatus() {
		return this.request<HealthWorkerStatus>("/health/worker/status");
	}

	async getLibrarySyncStatus() {
		return this.request<LibrarySyncStatus>("/health/library-sync/status");
	}

	async startLibrarySync() {
		return this.request<{ message: string }>("/health/library-sync/start", {
			method: "POST",
		});
	}

	async cancelLibrarySync() {
		return this.request<{ message: string }>("/health/library-sync/cancel", {
			method: "POST",
		});
	}

	async getPoolMetrics() {
		return this.request<PoolMetrics>("/system/pool/metrics");
	}

	async directHealthCheck(id: number) {
		return this.request<{
			message: string;
			id: number;
			file_path: string;
			old_status: string;
			new_status: string;
			checked_at: string;
			health_data: FileHealth;
		}>(`/health/${id}/check-now`, {
			method: "POST",
		});
	}

	async cancelHealthCheck(id: number) {
		return this.request<{
			message: string;
			id: number;
			file_path: string;
			old_status: string;
			new_status: string;
			cancelled_at: string;
			health_data: FileHealth;
		}>(`/health/${id}/cancel`, {
			method: "POST",
		});
	}

	// File metadata endpoints
	async getFileMetadata(path: string) {
		return this.request<FileMetadata>(`/files/info?path=${encodeURIComponent(path)}`);
	}

	async exportMetadataToNZB(path: string): Promise<Blob> {
		const url = `${this.baseURL}/files/export-nzb?path=${encodeURIComponent(path)}`;

		const response = await fetch(url, {
			credentials: "include",
			headers: {
				Accept: "application/x-nzb",
			},
		});

		if (!response.ok) {
			const errorData = await response.json();
			throw new APIError(
				response.status,
				errorData.message || `HTTP ${response.status}: ${response.statusText}`,
				errorData.details || "",
			);
		}

		return response.blob();
	}

	// Authentication endpoints
	async getCurrentUser() {
		return this.request<User>("/user");
	}

	async refreshToken() {
		return this.request<AuthResponse>("/user/refresh", {
			method: "POST",
		});
	}

	async logout() {
		return this.request<AuthResponse>("/user/logout", {
			method: "POST",
		});
	}

	async regenerateAPIKey() {
		return this.request<{ api_key: string; message: string }>("/user/api-key/regenerate", {
			method: "POST",
		});
	}

	async getUsers(params?: { limit?: number; offset?: number }) {
		const searchParams = new URLSearchParams();
		if (params?.limit) searchParams.set("limit", params.limit.toString());
		if (params?.offset) searchParams.set("offset", params.offset.toString());

		const query = searchParams.toString();
		return this.request<User[]>(`/users${query ? `?${query}` : ""}`);
	}

	async updateUserAdmin(userId: string, data: UserAdminUpdateRequest) {
		return this.request<AuthResponse>(`/users/${userId}/admin`, {
			method: "PUT",
			body: JSON.stringify(data),
		});
	}

	// Direct authentication methods
	async login(username: string, password: string) {
		return this.request<AuthResponse>("/auth/login", {
			method: "POST",
			body: JSON.stringify({ username, password }),
		});
	}

	async register(username: string, email: string | undefined, password: string) {
		return this.request<AuthResponse>("/auth/register", {
			method: "POST",
			body: JSON.stringify({
				username,
				email: email || undefined,
				password,
			}),
		});
	}

	async checkRegistrationStatus() {
		return this.request<{ registration_enabled: boolean; user_count: number }>(
			"/auth/registration-status",
		);
	}

	async getAuthConfig() {
		return this.request<{ login_required: boolean }>("/auth/config");
	}

	// Configuration endpoints
	async getConfig() {
		return this.request<ConfigResponse>("/config");
	}

	async updateConfig(config: ConfigUpdateRequest) {
		return this.request<ConfigResponse>("/config", {
			method: "PUT",
			body: JSON.stringify(config),
		});
	}

	async updateConfigSection(section: ConfigSection, config: ConfigUpdateRequest) {
		return this.request<ConfigResponse>(`/config/${section}`, {
			method: "PATCH",
			body: JSON.stringify(config),
		});
	}

	async validateConfig(config: ConfigValidateRequest) {
		return this.request<ConfigValidateResponse>("/config/validate", {
			method: "POST",
			body: JSON.stringify(config),
		});
	}

	async reloadConfig() {
		return this.request<ConfigResponse>("/config/reload", {
			method: "POST",
		});
	}

	// System endpoints
	async restartServer(force = false) {
		return this.request<{ message: string; timestamp: string }>("/system/restart", {
			method: "POST",
			body: JSON.stringify({ force }),
		});
	}

	// Provider endpoints
	async testProvider(data: ProviderTestRequest) {
		return this.request<ProviderTestResponse>("/providers/test", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	async createProvider(data: ProviderCreateRequest) {
		return this.request<ProviderConfig>("/providers", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	async updateProvider(id: string, data: Partial<ProviderUpdateRequest>) {
		return this.request<ProviderConfig>(`/providers/${id}`, {
			method: "PUT",
			body: JSON.stringify(data),
		});
	}

	async deleteProvider(id: string) {
		return this.request<{ message: string }>(`/providers/${id}`, {
			method: "DELETE",
		});
	}

	async reorderProviders(data: ProviderReorderRequest) {
		return this.request<ProviderConfig[]>("/providers/reorder", {
			method: "PUT",
			body: JSON.stringify(data),
		});
	}

	// Manual Scan endpoints
	async startManualScan(data: ManualScanRequest) {
		return this.request<ScanStatusResponse>("/import/scan", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	async getScanStatus() {
		return this.request<ScanStatusResponse>("/import/scan/status");
	}

	async cancelScan() {
		return this.request<ScanStatusResponse>("/import/scan", {
			method: "DELETE",
		});
	}

	// SABnzbd file upload endpoint
	async uploadNzbFile(file: File, apiKey: string): Promise<SABnzbdAddResponse> {
		const formData = new FormData();
		formData.append("nzbfile", file);

		const url = `/sabnzbd?mode=addfile&apikey=${encodeURIComponent(apiKey)}`;

		const response = await fetch(url, {
			method: "POST",
			body: formData,
			credentials: "include", // Include cookies for Safari compatibility
		});

		if (!response.ok) {
			throw new APIError(response.status, `Upload failed: ${response.statusText}`, "");
		}

		const data = await response.json();
		if (!data.status) {
			const err = data as APIError;
			throw new APIError(response.status, err.message || "Upload failed", err.details || "");
		}

		return data;
	}

	// Native upload endpoint using JWT authentication
	async uploadToQueue(
		file: File,
		category?: string,
		priority?: number,
	): Promise<APIResponse<QueueItem>> {
		const formData = new FormData();
		formData.append("file", file);
		if (category) {
			formData.append("category", category);
		}
		if (priority !== undefined) {
			formData.append("priority", priority.toString());
		}

		return this.request<APIResponse<QueueItem>>("/queue/upload", {
			method: "POST",
			body: formData,
			// Don't set Content-Type header - let browser set it with boundary for multipart/form-data
			headers: {},
		});
	}
}

// Export a default instance
export const apiClient = new APIClient();
