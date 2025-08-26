import type {
	APIResponse,
	AuthResponse,
	FileHealth,
	FileMetadata,
	HealthCheckRequest,
	HealthStats,
	HealthWorkerStatus,
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

	constructor(status: number, message: string) {
		super(message);
		this.status = status;
		this.name = "APIError";
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
			headers: {
				"Content-Type": "application/json",
				...options.headers,
			},
			...options,
		};

		try {
			const response = await fetch(url, config);

			if (!response.ok) {
				throw new APIError(response.status, `HTTP ${response.status}: ${response.statusText}`);
			}

			const data: APIResponse<T> = await response.json();

			if (!data.success) {
				throw new APIError(response.status, data.error || "API request failed");
			}

			return data.data as T;
		} catch (error) {
			if (error instanceof APIError) {
				throw error;
			}
			throw new APIError(0, error instanceof Error ? error.message : "Network error");
		}
	}

	private async requestWithMeta<T>(
		endpoint: string,
		options: RequestInit = {},
	): Promise<APIResponse<T>> {
		const url = `${this.baseURL}${endpoint}`;

		const config: RequestInit = {
			headers: {
				"Content-Type": "application/json",
				...options.headers,
			},
			...options,
		};

		try {
			const response = await fetch(url, config);

			if (!response.ok) {
				throw new APIError(response.status, `HTTP ${response.status}: ${response.statusText}`);
			}

			const data: APIResponse<T> = await response.json();

			if (!data.success) {
				throw new APIError(response.status, data.error || "API request failed");
			}

			return data;
		} catch (error) {
			if (error instanceof APIError) {
				throw error;
			}
			throw new APIError(0, error instanceof Error ? error.message : "Network error");
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

	async retryQueueItem(id: number, resetRetryCount?: boolean) {
		return this.request<QueueItem>(`/queue/${id}/retry`, {
			method: "POST",
			body: JSON.stringify({ reset_retry_count: resetRetryCount }),
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

	// Health endpoints
	async getHealth(params?: {
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
		return this.requestWithMeta<FileHealth[]>(`/health${query ? `?${query}` : ""}`);
	}

	async getHealthItem(id: string) {
		return this.request<FileHealth>(`/health/${encodeURIComponent(id)}`);
	}

	async deleteHealthItem(id: string) {
		return this.request<FileHealth>(`/health/${encodeURIComponent(id)}`, {
			method: "DELETE",
		});
	}

	async retryHealthItem(id: string, resetStatus?: boolean) {
		return this.request<FileHealth>(`/health/${encodeURIComponent(id)}/retry`, {
			method: "POST",
			body: JSON.stringify({ reset_status: resetStatus }),
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

	async cleanupHealth(params?: { older_than?: string; status?: string }) {
		return this.request<HealthStats>("/health/cleanup", {
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

	async getPoolMetrics() {
		return this.request<PoolMetrics>("/system/pool/metrics");
	}

	async directHealthCheck(filePath: string) {
		return this.request<{
			message: string;
			file_path: string;
			old_status: string;
			new_status: string;
			checked_at: string;
			health_data: FileHealth;
		}>(`/health/${encodeURIComponent(filePath)}/check-now`, {
			method: "POST",
		});
	}

	async cancelHealthCheck(filePath: string) {
		return this.request<{
			message: string;
			file_path: string;
			old_status: string;
			new_status: string;
			cancelled_at: string;
			health_data: FileHealth;
		}>(`/health/${encodeURIComponent(filePath)}/cancel`, {
			method: "POST",
		});
	}

	// File metadata endpoints
	async getFileMetadata(path: string) {
		return this.request<FileMetadata>(`/files/info?path=${encodeURIComponent(path)}`);
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

		const url = `/api/sabnzbd?mode=addfile&apikey=${encodeURIComponent(apiKey)}`;

		const response = await fetch(url, {
			method: "POST",
			body: formData,
		});

		if (!response.ok) {
			throw new APIError(response.status, `Upload failed: ${response.statusText}`);
		}

		const data = await response.json();
		if (!data.status) {
			throw new APIError(response.status, data.error || "Upload failed");
		}

		return data;
	}
}

// Export a default instance
export const apiClient = new APIClient();
