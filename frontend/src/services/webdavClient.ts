import { AuthType, createClient, type FileStat } from "webdav";
import type {
	WebDAVConnection,
	WebDAVDirectory,
	WebDAVFile,
} from "../types/webdav";

export class WebDAVClient {
	private client: ReturnType<typeof createClient> | null = null;

	connect(connection: WebDAVConnection) {
		this.client = createClient(connection.url, {
			authType: AuthType.Auto,
			username: connection.username,
			password: connection.password,
		});
	}

	isConnected(): boolean {
		return this.client !== null;
	}

	async listDirectory(path: string = "/"): Promise<WebDAVDirectory> {
		if (!this.client) {
			throw new Error("WebDAV client not connected");
		}

		try {
			const contents = await this.client.getDirectoryContents(path);
			const files: WebDAVFile[] = (contents as FileStat[]).map((item) => ({
				filename: item.filename,
				basename: item.basename,
				lastmod: item.lastmod,
				size: item.size || 0,
				type: item.type as "file" | "directory",
				etag: item.etag ?? undefined,
				mime: item.mime,
			}));

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
			throw new Error(`Failed to list directory: ${path}`);
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
			throw new Error(`Failed to download file: ${path}`);
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
			throw new Error(`Failed to get file info: ${path}`);
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
			throw new Error(`Failed to delete file: ${path}`);
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
			return false;
		}
	}
}

// Export singleton instance
export const webdavClient = new WebDAVClient();
