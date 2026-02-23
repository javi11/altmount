export interface WebDAVFile {
	filename: string;
	basename: string;
	lastmod: string;
	size: number;
	type: "file" | "directory";
	etag?: string | null;
	mime?: string;
	library_path?: string;
}

export interface WebDAVDirectory {
	path: string;
	files: WebDAVFile[];
}

export interface BreadcrumbItem {
	name: string;
	path: string;
}

export interface FileOperation {
	type: "download" | "delete" | "info";
	file: WebDAVFile;
	path: string;
}
