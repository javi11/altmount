export type UpdateChannel = "latest" | "dev";

export interface UpdateStatusResponse {
	current_version: string;
	git_commit?: string;
	channel: UpdateChannel;
	latest_version?: string;
	update_available: boolean;
	release_url?: string;
}
