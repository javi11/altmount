import { AuthType, createClient, type FileStat } from "webdav";
import type { WebDAVDirectory, WebDAVFile } from "../types/webdav";

export class WebDAVClient {
	private client: ReturnType<typeof createClient> | null = null;

	// Parse and enhance error messages for better handling
	private parseError(error: unknown, operation: string, path?: string): Error {
		const pathInfo = path ? ` (path: ${path})` : "";
		const err = error as {
			status?: number;
			response?: { status: number };
			name?: string;
			message?: string;
		};

		// Handle network/connection errors
		if (err.name === "TypeError" && err.message?.includes("Failed to fetch")) {
			return new Error(
				`Network error during ${operation}${pathInfo}: Unable to connect to WebDAV server`,
			);
		}

		// Handle HTTP status errors
		if (err.status || err.response?.status) {
			const status = err.status || err.response?.status;
			switch (status) {
				case 401:
					return new Error(`401 Unauthorized: Authentication required for ${operation}${pathInfo}`);
				case 403:
					return new Error(`403 Forbidden: Access denied for ${operation}${pathInfo}`);
				case 404:
					return new Error(`404 Not Found: Path does not exist for ${operation}${pathInfo}`);
				case 500:
					return new Error(`500 Server Error: WebDAV server error during ${operation}${pathInfo}`);
				case 502:
					return new Error(
						`502 Bad Gateway: WebDAV server unavailable during ${operation}${pathInfo}`,
					);
				case 503:
					return new Error(
						`503 Service Unavailable: WebDAV server overloaded during ${operation}${pathInfo}`,
					);
				default:
					return new Error(`${status} Error: HTTP error during ${operation}${pathInfo}`);
			}
		}

		// Handle timeout errors
		if (err.message?.toLowerCase().includes("timeout")) {
			return new Error(`Timeout error during ${operation}${pathInfo}: Request took too long`);
		}

		// Handle other WebDAV-specific errors
		if (err.message) {
			return new Error(`${operation} failed${pathInfo}: ${err.message}`);
		}

		// Fallback
		return new Error(`Unknown error during ${operation}${pathInfo}`);
	}

	connect() {
		// Use relative /webdav path and let browser handle authentication via cookies
		// No credentials needed since we use cookie-based authentication
		const clientOptions = {
			authType: AuthType.Auto,
			// Add timeout to prevent hanging requests
			timeout: 10000, // 10 seconds
			// Add headers for better error handling
			headers: {
				Accept: "application/xml,text/xml,*/*",
				"Content-Type": "application/xml; charset=utf-8",
			},
		};

		this.client = createClient("/webdav", clientOptions);
	}

	isConnected(): boolean {
		return this.client !== null;
	}

	async listDirectory(path = "/"): Promise<WebDAVDirectory> {
		if (!this.client) {
			throw new Error("WebDAV client not connected");
		}

		try {
			const contents = await this.client.getDirectoryContents(path);
			
			// Handle empty directories - contents could be empty array or null
			let files: WebDAVFile[] = [];
			if (Array.isArray(contents) && contents.length > 0) {
				files = (contents as FileStat[]).map((item) => ({
					filename: item.filename,
					basename: item.basename,
					lastmod: item.lastmod,
					size: item.size || 0,
					type: item.type as "file" | "directory",
					etag: item.etag ?? undefined,
					mime: item.mime,
				}));
			}

			return {
				path,
				files: files.sort((a, b) => {
					// Directories first, then files
					if (a.type !== b.type) {
						return a.type === "directory" ? -1 : 1;
					}
					// Alphabetical within type
					return a.basename.localeCompare(b.basename);
				}),
			};
		} catch (error) {
			console.error("Failed to list directory:", error);
			throw this.parseError(error, "list directory", path);
		}
	}

	async downloadFile(path: string): Promise<Blob> {
		if (!this.client) {
			throw new Error("WebDAV client not connected");
		}

		try {
			const buffer = await this.client.getFileContents(path, {
				format: "binary",
			});
			return new Blob([buffer as ArrayBuffer]);
		} catch (error) {
			console.error("Failed to download file:", error);
			throw this.parseError(error, "download file", path);
		}
	}

	async getFileContents(path: string, asText = false): Promise<Blob | string> {
		if (!this.client) {
			throw new Error("WebDAV client not connected");
		}

		try {
			if (asText) {
				const content = await this.client.getFileContents(path, {
					format: "text",
				});
				return content as string;
			}
			const buffer = await this.client.getFileContents(path, {
				format: "binary",
			});
			return new Blob([buffer as ArrayBuffer]);
		} catch (error) {
			console.error("Failed to get file contents:", error);
			throw this.parseError(error, "get file contents", path);
		}
	}

	async getFileInfo(path: string): Promise<WebDAVFile> {
		if (!this.client) {
			throw new Error("WebDAV client not connected");
		}

		try {
			const stat = (await this.client.stat(path)) as FileStat;
			return {
				filename: stat.filename,
				basename: stat.basename,
				lastmod: stat.lastmod,
				size: stat.size || 0,
				type: stat.type as "file" | "directory",
				etag: stat.etag ?? undefined,
				mime: stat.mime,
			};
		} catch (error) {
			console.error("Failed to get file info:", error);
			throw this.parseError(error, "get file info", path);
		}
	}

	async deleteFile(path: string): Promise<void> {
		if (!this.client) {
			throw new Error("WebDAV client not connected");
		}

		try {
			await this.client.deleteFile(path);
		} catch (error) {
			console.error("Failed to delete file:", error);
			throw this.parseError(error, "delete file", path);
		}
	}

	async testConnection(): Promise<boolean> {
		if (!this.client) {
			return false;
		}

		try {
			await this.client.getDirectoryContents("/");
			return true;
		} catch (error) {
			console.error("WebDAV connection test failed:", error);
			// Don't throw error for connection test, just return false
			return false;
		}
	}
}

// Export singleton instance
export const webdavClient = new WebDAVClient();
