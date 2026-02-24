export interface UpdateStatusResponse {
	current_version: string;
	latest_version?: string;
	update_available: boolean;
	docker_available: boolean;
	release_notes?: string;
	release_url?: string;
}
