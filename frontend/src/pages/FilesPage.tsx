import { Server, Wifi } from "lucide-react";
import { useState } from "react";
import { FileExplorer } from "../components/files/FileExplorer";
import { useWebDAVConnection } from "../hooks/useWebDAV";

export function FilesPage() {
	const { isConnected, connect, isConnecting, connectionError } =
		useWebDAVConnection();
	const [showConnectionForm, setShowConnectionForm] = useState(!isConnected);
	const [connectionForm, setConnectionForm] = useState({
		url: "http://localhost:8080", // Default WebDAV port
		username: "",
		password: "",
	});

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();
		connect({
			url: connectionForm.url,
			username: connectionForm.username,
			password: connectionForm.password,
		});
		setShowConnectionForm(false);
	};

	const handleFormChange = (field: string, value: string) => {
		setConnectionForm((prev) => ({
			...prev,
			[field]: value,
		}));
	};

	if (showConnectionForm) {
		return (
			<div className="max-w-md mx-auto mt-16">
				<div className="card bg-base-100 shadow-xl">
					<div className="card-body">
						<div className="flex items-center space-x-3 mb-6">
							<Server className="h-8 w-8 text-primary" />
							<div>
								<h2 className="card-title">Connect to WebDAV</h2>
								<p className="text-base-content/70">
									Enter your WebDAV server details
								</p>
							</div>
						</div>

						<form onSubmit={handleSubmit} className="space-y-4">
							<div className="form-control">
								<label className="label" htmlFor="url">
									<span className="label-text">Server URL</span>
								</label>
								<input
									id="url"
									type="url"
									placeholder="http://localhost:8080"
									className="input input-bordered"
									value={connectionForm.url}
									onChange={(e) => handleFormChange("url", e.target.value)}
									required
								/>
							</div>

							<div className="form-control">
								<label className="label" htmlFor="username">
									<span className="label-text">Username</span>
								</label>
								<input
									id="username"
									type="text"
									placeholder="Optional"
									className="input input-bordered"
									value={connectionForm.username}
									onChange={(e) => handleFormChange("username", e.target.value)}
								/>
							</div>

							<div className="form-control">
								<label className="label" htmlFor="password">
									<span className="label-text">Password</span>
								</label>
								<input
									id="password"
									type="password"
									placeholder="Optional"
									className="input input-bordered"
									value={connectionForm.password}
									onChange={(e) => handleFormChange("password", e.target.value)}
								/>
							</div>

							{connectionError && (
								<div className="alert alert-error">
									<span>{connectionError.message}</span>
								</div>
							)}

							<div className="card-actions justify-end">
								{isConnected && (
									<button
										type="button"
										className="btn btn-ghost"
										onClick={() => setShowConnectionForm(false)}
									>
										Cancel
									</button>
								)}
								<button
									type="submit"
									className="btn btn-primary"
									disabled={isConnecting}
								>
									<Wifi className="h-4 w-4" />
									{isConnecting ? "Connecting..." : "Connect"}
								</button>
							</div>
						</form>
					</div>
				</div>
			</div>
		);
	}

	return (
		<FileExplorer
			isConnected={isConnected}
			onConnect={() => setShowConnectionForm(true)}
		/>
	);
}
