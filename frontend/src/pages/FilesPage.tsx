import {
	Book,
	Film,
	Folder,
	Gamepad2,
	HardDrive,
	History,
	Music,
	Tv,
	Wifi,
	WifiOff,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { FileExplorer } from "../components/files/FileExplorer";
import { useConfig } from "../hooks/useConfig";
import { useWebDAVConnection } from "../hooks/useWebDAV";

type FileView = string;

const SECONDARY_SHORTCUTS = [{ id: "recent", title: "Recently Added", icon: History }];

export function FilesPage() {
	const { data: config } = useConfig();
	const { isConnected, hasConnectionFailed, connect, isConnecting, connectionError } =
		useWebDAVConnection();

	const [activeView, setActiveView] = useState<FileView>("all");
	const [initialPath, setInitialPath] = useState("/");

	const fileShortcuts = useMemo(() => {
		const shortcuts = [{ id: "all", title: "All Files", path: "/", icon: Folder }];

		if (config?.sabnzbd?.categories) {
			const strategy = config.import?.import_strategy || "NONE";
			let basePath = "/";

			if (strategy !== "NONE") {
				// With SYMLINK/STRM strategies, the virtual files remain in complete_dir
				basePath = config.sabnzbd?.complete_dir || "/";
			}

			// Ensure valid base path slashes
			if (basePath && !basePath.startsWith("/")) {
				basePath = `/${basePath}`;
			}
			if (basePath?.endsWith("/") && basePath !== "/") {
				basePath = basePath.slice(0, -1);
			}

			config.sabnzbd.categories.forEach((cat) => {
				if (cat.name.toLowerCase() === "default") return;

				let icon = Folder;
				const lowerName = cat.name.toLowerCase();
				if (lowerName.includes("movie") || lowerName.includes("film")) icon = Film;
				else if (
					lowerName.includes("tv") ||
					lowerName.includes("show") ||
					lowerName.includes("anime")
				)
					icon = Tv;
				else if (
					lowerName.includes("music") ||
					(lowerName.includes("audio") && !lowerName.includes("audiobook"))
				)
					icon = Music;
				else if (lowerName.includes("book") || lowerName.includes("audiobook")) icon = Book;
				else if (lowerName.includes("game")) icon = Gamepad2;

				let catPath = basePath === "/" ? `/${cat.name}` : `${basePath}/${cat.name}`;
				catPath = catPath.replace(/\/\//g, "/");

				shortcuts.push({
					id: cat.name,
					title: cat.name.charAt(0).toUpperCase() + cat.name.slice(1),
					path: catPath,
					icon: icon,
				});
			});
		} else {
			// Fallback while loading
			shortcuts.push({ id: "movies", title: "Movies", path: "/movies", icon: Film });
			shortcuts.push({ id: "tv", title: "TV Shows", path: "/tv", icon: Tv });
		}

		return shortcuts;
	}, [config]);

	// Track connection attempts to prevent rapid retries
	const connectionAttempted = useRef(false);

	// Stable connect function to prevent useEffect loops
	const handleConnect = useCallback(() => {
		if (!connectionAttempted.current && !isConnected && !isConnecting) {
			connectionAttempted.current = true;
			connect();
		}
	}, [isConnected, isConnecting, connect]);

	// Auto-connect on page load
	useEffect(() => {
		handleConnect();
	}, [handleConnect]);

	// Reset connection attempt flag when connection state changes
	useEffect(() => {
		if (isConnected || connectionError) {
			connectionAttempted.current = false;
		}
	}, [isConnected, connectionError]);

	const handleRetryConnection = useCallback(() => {
		connectionAttempted.current = false;
		connect();
	}, [connect]);

	const handleViewChange = (viewId: FileView, path?: string) => {
		setActiveView(viewId);
		if (path) {
			setInitialPath(path);
		}
	};

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
				<div className="flex items-center space-x-3">
					<div className="rounded-xl bg-primary/10 p-2">
						<HardDrive className="h-8 w-8 text-primary" />
					</div>
					<div>
						<h1 className="font-bold text-3xl tracking-tight">File Explorer</h1>
						<p className="text-base-content/60 text-sm">
							Browse and manage your cloud media library
						</p>
					</div>
				</div>

				<div className="flex items-center gap-3">
					{isConnecting ? (
						<div className="badge badge-ghost gap-2 px-3 py-3 font-medium text-xs">
							<span className="loading loading-spinner loading-xs" />
							Connecting
						</div>
					) : isConnected ? (
						<div className="badge badge-success badge-outline gap-2 px-3 py-3 font-semibold text-xs">
							<Wifi className="h-3.5 w-3.5" />
							Connected
						</div>
					) : (
						<div className="badge badge-error badge-outline gap-2 px-3 py-3 font-semibold text-xs">
							<WifiOff className="h-3.5 w-3.5" />
							Offline
						</div>
					)}
				</div>
			</div>

			<div className="grid grid-cols-1 gap-6 lg:grid-cols-12">
				{/* Sidebar Navigation */}
				<div className="lg:col-span-3 xl:col-span-2">
					{" "}
					<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
						<div className="card-body p-2 sm:p-4">
							<div className="space-y-6">
								<div>
									<h3 className="mb-2 px-4 font-bold text-base-content/40 text-xs uppercase tracking-widest">
										Library
									</h3>
									<ul className="menu menu-md gap-1 p-0">
										{fileShortcuts.map((item) => {
											const Icon = item.icon;
											const isActive = activeView === item.id;
											return (
												<li key={item.id}>
													<button
														type="button"
														className={`flex items-center gap-3 rounded-lg px-4 py-3 transition-all ${
															isActive
																? "bg-primary font-semibold text-primary-content shadow-md shadow-primary/20"
																: "hover:bg-base-200"
														}`}
														onClick={() => handleViewChange(item.id as FileView, item.path)}
													>
														<Icon className={`h-5 w-5 ${isActive ? "" : "text-base-content/60"}`} />
														<span className="text-sm">{item.title}</span>
													</button>
												</li>
											);
										})}
									</ul>
								</div>

								<div>
									<h3 className="mb-2 px-4 font-bold text-base-content/40 text-xs uppercase tracking-widest">
										Filters
									</h3>
									<ul className="menu menu-md gap-1 p-0">
										{SECONDARY_SHORTCUTS.map((item) => {
											const Icon = item.icon;
											const isActive = activeView === item.id;
											return (
												<li key={item.id}>
													<button
														type="button"
														className={`flex items-center gap-3 rounded-lg px-4 py-3 transition-all ${
															isActive
																? "bg-primary font-semibold text-primary-content shadow-md shadow-primary/20"
																: "hover:bg-base-200"
														}`}
														onClick={() => handleViewChange(item.id as FileView)}
													>
														<Icon className={`h-5 w-5 ${isActive ? "" : "text-base-content/60"}`} />
														<span className="text-sm">{item.title}</span>
													</button>
												</li>
											);
										})}
									</ul>
								</div>
							</div>
						</div>
					</div>
				</div>

				{/* Main Content */}
				<div className="lg:col-span-9 xl:col-span-10">
					{" "}
					<div className="card min-h-[600px] border-2 border-base-300/50 bg-base-100 shadow-md">
						<div className="card-body p-0 sm:p-0">
							<div className="p-4 sm:p-8">
								<FileExplorer
									isConnected={isConnected}
									hasConnectionFailed={hasConnectionFailed}
									isConnecting={isConnecting}
									connectionError={connectionError}
									onRetryConnection={handleRetryConnection}
									initialPath={initialPath}
									activeView={activeView}
								/>
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}
