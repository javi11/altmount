import { Upload, FolderInput, HardDrive } from "lucide-react";
import { useState } from "react";
import { useToast } from "../contexts/ToastContext";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { FileBrowserModal } from "../components/files/FileBrowserModal"; // Import the modal

export function ImportPage() {
    const [selectedDbPath, setSelectedDbPath] = useState("");
    const [rootFolder, setRootFolder] = useState("");
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState<Error | null>(null);
    const [result, setResult] = useState<{ added: number; failed: number; total: number } | null>(null);
    const { showToast } = useToast();
    const [isFileBrowserOpen, setIsFileBrowserOpen] = useState(false); // State for modal

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!selectedDbPath || !rootFolder) return;

        setIsLoading(true);
        setError(null);
        setResult(null);

        const formData = new FormData();
        formData.append("dbPath", selectedDbPath); // Use selected path
        formData.append("rootFolder", rootFolder);

        try {
            const token = localStorage.getItem("token");
            const headers: HeadersInit = {};
            if (token) {
                headers["Authorization"] = `Bearer ${token}`;
            }

            const response = await fetch("/api/import/nzbdav", {
                method: "POST",
                headers: headers,
                body: formData,
            });

            if (!response.ok) {
                const data = await response.json().catch(() => ({}));
                throw new Error(data.message || "Failed to import database");
            }

            const data = await response.json();
            setResult(data.data);
            showToast({
                title: "Import Successful",
                message: `Successfully imported ${data.data.added} items`,
                type: "success",
            });
            
            // Optionally reset form
            // setSelectedDbPath("");
            // setRootFolder("");

        } catch (err: any) {
            setError(err);
            showToast({
                title: "Import Failed",
                message: err.message || "An error occurred",
                type: "error",
            });
        } finally {
            setIsLoading(false);
        }
    };

    const handleFileSelect = (path: string) => {
        setSelectedDbPath(path);
    };

    return (
        <div className="space-y-6">
            <div>
                <h1 className="font-bold text-3xl">Import from NZBDav</h1>
                <p className="text-base-content/70">
                    Import your existing NZBDav database to populate the library.
                </p>
            </div>

            {error && <ErrorAlert error={error} />}

            {result && (
                <div className="alert alert-success">
                    <div>
                        <h3 className="font-bold">Import Completed</h3>
                        <div className="text-sm">
                            <p>Total processed: {result.total}</p>
                            <p>Added to queue: {result.added}</p>
                            <p>Failed: {result.failed}</p>
                        </div>
                    </div>
                </div>
            )}

            <div className="card bg-base-100 shadow-lg max-w-2xl">
                <div className="card-body">
                    <form onSubmit={handleSubmit} className="space-y-4">
                        <div className="form-control w-full">
                            <label className="label">
                                <span className="label-text font-medium flex items-center gap-2">
                                    <FolderInput className="h-4 w-4" />
                                    Target Directory Name
                                </span>
                            </label>
                            <input
                                type="text"
                                placeholder="e.g. MyLibrary"
                                className="input input-bordered w-full"
                                value={rootFolder}
                                onChange={(e) => setRootFolder(e.target.value)}
                                required
                            />
                            <label className="label">
                                <span className="label-text-alt text-base-content/60">
                                    This will create /movies and /tv subdirectories under this name.
                                </span>
                            </label>
                        </div>

                        <div className="form-control w-full">
                            <label className="label">
                                <span className="label-text font-medium flex items-center gap-2">
                                    <HardDrive className="h-4 w-4" />
                                    Select Database File from Server
                                </span>
                            </label>
                            <div className="join w-full">
                                <input
                                    type="text"
                                    placeholder="e.g. /data/nzbdav/db.sqlite"
                                    className="input input-bordered join-item w-full"
                                    value={selectedDbPath}
                                    onChange={(e) => setSelectedDbPath(e.target.value)}
                                    required
                                />
                                <button
                                    type="button"
                                    className="btn btn-primary join-item"
                                    onClick={() => setIsFileBrowserOpen(true)}
                                >
                                    Browse
                                </button>
                            </div>
                        </div>

                        <div className="card-actions justify-end mt-4">
                            <button
                                type="submit"
                                className="btn btn-primary"
                                disabled={isLoading || !selectedDbPath || !rootFolder}
                            >
                                {isLoading ? (
                                    <span className="loading loading-spinner"></span>
                                ) : (
                                    <Upload className="h-4 w-4" />
                                )}
                                Start Import
                            </button>
                        </div>
                    </form>
                </div>
            </div>

            {/* File Browser Modal */}
            <FileBrowserModal
                isOpen={isFileBrowserOpen}
                onClose={() => setIsFileBrowserOpen(false)}
                onSelect={handleFileSelect}
                filterExtension=".sqlite" // Only show sqlite files
            />
        </div>
    );
}
