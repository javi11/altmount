import {
	AlertTriangle,
	Check,
	Cog,
	Download,
	Folder,
	Globe,
	HardDrive,
	Radio,
	RefreshCw,
	Settings,
	Shield,
	X,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ArrsConfigSection } from "../components/config/ArrsConfigSection";
import { AuthConfigSection } from "../components/config/AuthConfigSection";
import { ComingSoonSection } from "../components/config/ComingSoonSection";
import { HealthConfigSection } from "../components/config/HealthConfigSection";
import { MetadataConfigSection } from "../components/config/MetadataConfigSection";
import { ProvidersConfigSection } from "../components/config/ProvidersConfigSection";
import { RCloneConfigSection } from "../components/config/RCloneConfigSection";
import { SABnzbdConfigSection } from "../components/config/SABnzbdConfigSection";
import { StreamingConfigSection } from "../components/config/StreamingConfigSection";
import { SystemConfigSection } from "../components/config/SystemConfigSection";
import { WebDAVConfigSection } from "../components/config/WebDAVConfigSection";
import { ImportConfigSection } from "../components/config/WorkersConfigSection";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingSpinner } from "../components/ui/LoadingSpinner";
import { RestartRequiredBanner } from "../components/ui/RestartRequiredBanner";
import { useConfirm } from "../contexts/ModalContext";
import {
	useConfig,
	useLibrarySyncNeeded,
	useReloadConfig,
	useRestartServer,
	useTriggerLibrarySync,
	useUpdateConfigSection,
} from "../hooks/useConfig";
import type {
	ArrsConfig,
	AuthConfig,
	ConfigSection,
	HealthConfig,
	ImportConfig,
	LogFormData,
	MetadataConfig,
	RCloneMountFormData,
	RCloneRCFormData,
	SABnzbdConfig,
	StreamingConfig,
	WebDAVConfig,
} from "../types/config";
import { CONFIG_SECTIONS } from "../types/config";

// Helper function to get icon component
const getIconComponent = (iconName: string) => {
	const iconMap = {
		Globe,
		Folder,
		Download,
		Shield,
		Cog,
		Radio,
		HardDrive,
	};
	return iconMap[iconName as keyof typeof iconMap] || Settings;
};

export function ConfigurationPage() {
	const { data: config, isLoading, error, refetch } = useConfig();
	const reloadConfig = useReloadConfig();
	const restartServer = useRestartServer();
	const updateConfigSection = useUpdateConfigSection();
	const { data: syncNeeded } = useLibrarySyncNeeded();
	const triggerLibrarySync = useTriggerLibrarySync();
	const { confirmAction } = useConfirm();
	const navigate = useNavigate();
	const { section } = useParams<{ section: string }>();

	// Get active section from URL parameter, default to webdav
	const activeSection = (() => {
		if (!section) return "webdav";
		const validSections = Object.keys(CONFIG_SECTIONS) as (ConfigSection | "system")[];
		return validSections.includes(section as ConfigSection | "system")
			? (section as ConfigSection | "system")
			: "webdav";
	})();

	// Redirect to default section if no section is specified
	useEffect(() => {
		if (!section) {
			navigate("/config/webdav", { replace: true });
		}
	}, [section, navigate]);

	const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
	const [restartRequiredConfigs, setRestartRequiredConfigs] = useState<string[]>([]);
	const [isRestartBannerDismissed, setIsRestartBannerDismissed] = useState(() => {
		// Initialize from session storage on component mount
		return sessionStorage.getItem("restartBannerDismissed") === "true";
	});

	// Helper functions for restart required state
	const addRestartRequiredConfig = (configName: string) => {
		setRestartRequiredConfigs((prev) => (prev.includes(configName) ? prev : [...prev, configName]));
		setIsRestartBannerDismissed(false);
	};

	const handleDismissRestartBanner = () => {
		setIsRestartBannerDismissed(true);
		sessionStorage.setItem("restartBannerDismissed", "true");
	};

	// Clear restart state on config reload (indicates server restart)
	const handleReloadConfig = async () => {
		try {
			await reloadConfig.mutateAsync();
			setHasUnsavedChanges(false);
			setRestartRequiredConfigs([]);
			setIsRestartBannerDismissed(false);
			sessionStorage.removeItem("restartBannerDismissed");
		} catch (error) {
			console.error("Failed to reload configuration:", error);
		}
	};

	// Handle server restart
	const handleRestartServer = async () => {
		const confirmed = await confirmAction(
			"Restart Server",
			"This will restart the entire server. All active connections will be lost. Continue?",
			{
				type: "error",
				confirmText: "Restart Server",
				confirmButtonClass: "btn-error",
			},
		);
		if (!confirmed) {
			return;
		}

		try {
			await restartServer.mutateAsync(false);
			// Clear local state since server is restarting
			setHasUnsavedChanges(false);
			setRestartRequiredConfigs([]);
			setIsRestartBannerDismissed(false);
			sessionStorage.removeItem("restartBannerDismissed");

			// Wait a bit for the server to restart, then reload the page
			setTimeout(() => {
				window.location.reload();
			}, 3000);
		} catch (error) {
			console.error("Failed to restart server:", error);
		}
	};

	// Handle configuration updates with restart detection
	const handleConfigUpdate = async (
		section: string,
		data:
			| WebDAVConfig
			| AuthConfig
			| StreamingConfig
			| HealthConfig
			| ImportConfig
			| MetadataConfig
			| RCloneRCFormData
			| RCloneMountFormData
			| LogFormData
			| SABnzbdConfig
			| ArrsConfig
			| { mount_path: string }
			| { rclone: RCloneMountFormData; mount_path: string },
	) => {
		try {
			if (section === "webdav" && config) {
				const webdavData = data as WebDAVConfig;
				const portChanged = webdavData.port !== config.webdav.port;

				await updateConfigSection.mutateAsync({
					section: "webdav",
					config: { webdav: webdavData },
				});

				// Only add restart requirement after successful update
				if (portChanged) {
					addRestartRequiredConfig("WebDAV Port");
				}
			} else if (section === "auth") {
				await updateConfigSection.mutateAsync({
					section: "auth",
					config: { auth: data as AuthConfig },
				});
			} else if (section === "streaming") {
				await updateConfigSection.mutateAsync({
					section: "streaming",
					config: { streaming: data as StreamingConfig },
				});
			} else if (section === "import" && config) {
				const importData = data as ImportConfig;
				const workersChanged =
					importData.max_processor_workers !== config.import.max_processor_workers;

				await updateConfigSection.mutateAsync({
					section: "import",
					config: { import: importData },
				});

				// Only add restart requirement after successful update
				if (workersChanged) {
					addRestartRequiredConfig("Import Max Processor Workers");
				}
			} else if (section === "metadata" && config) {
				const metadataData = data as MetadataConfig;
				const rootPathChanged = metadataData.root_path !== config.metadata.root_path;

				await updateConfigSection.mutateAsync({
					section: "metadata",
					config: { metadata: metadataData },
				});

				// Only add restart requirement after successful update
				if (rootPathChanged) {
					addRestartRequiredConfig("Metadata Root Path");
				}
			} else if (section === "rclone") {
				await updateConfigSection.mutateAsync({
					section: "rclone",
					config: { rclone: data as RCloneMountFormData },
				});
			} else if (section === "rclone_with_path") {
				// Handle combined RClone settings + mount path to avoid validation errors
				const combinedData = data as { rclone: RCloneMountFormData; mount_path: string };
				await updateConfigSection.mutateAsync({
					section: "rclone",
					config: {
						rclone: combinedData.rclone,
						mount_path: combinedData.mount_path,
					},
				});
			} else if (section === "mount_path") {
				// For mount_path, we need to update the system section with mount_path
				const mountPathData = data as { mount_path: string };
				await updateConfigSection.mutateAsync({
					section: "system",
					config: mountPathData,
				});
			} else if (section === "sabnzbd") {
				await updateConfigSection.mutateAsync({
					section: "sabnzbd",
					config: { sabnzbd: data as SABnzbdConfig },
				});
			} else if (section === "arrs") {
				await updateConfigSection.mutateAsync({
					section: "arrs",
					config: { arrs: data as ArrsConfig },
				});
			} else if (section === "health") {
				await updateConfigSection.mutateAsync({
					section: "health",
					config: { health: data as HealthConfig },
				});
			} else if (section === "log") {
				await updateConfigSection.mutateAsync({
					section: "system",
					config: { log: data as LogFormData },
				});
			}
		} catch (error) {
			// If update fails, don't show restart banner
			console.error("Failed to update configuration:", error);
			throw error; // Re-throw to let the component handle the error
		}
	};

	if (isLoading) {
		return (
			<div className="flex min-h-[400px] items-center justify-center">
				<LoadingSpinner size="lg" />
			</div>
		);
	}

	if (error) {
		return (
			<div className="space-y-4">
				<h1 className="font-bold text-3xl">Configuration</h1>
				<ErrorAlert error={error as Error} onRetry={() => refetch()} />
			</div>
		);
	}

	if (!config) {
		return (
			<div className="space-y-4">
				<h1 className="font-bold text-3xl">Configuration</h1>
				<div className="alert alert-warning">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">Configuration Not Available</div>
						<div className="text-sm">
							Unable to load configuration. Please check the server status.
						</div>
					</div>
				</div>
			</div>
		);
	}

	const sections = Object.entries(CONFIG_SECTIONS) as [
		ConfigSection | "system",
		(typeof CONFIG_SECTIONS)[ConfigSection],
	][];

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex items-center justify-between">
				<div className="flex items-center space-x-3">
					<Settings className="h-8 w-8 text-primary" />
					<div>
						<h1 className="font-bold text-3xl">Configuration</h1>
						<p className="text-base-content/70">Manage system settings and preferences</p>
					</div>
				</div>

				<div className="flex items-center space-x-2">
					{hasUnsavedChanges && (
						<div className="badge badge-warning">
							<AlertTriangle className="mr-1 h-4 w-4" />
							Unsaved Changes
						</div>
					)}

					<button
						type="button"
						className="btn btn-outline btn-sm"
						onClick={handleReloadConfig}
						disabled={reloadConfig.isPending}
					>
						{reloadConfig.isPending ? (
							<LoadingSpinner size="sm" />
						) : (
							<RefreshCw className="h-4 w-4" />
						)}
						Reload
					</button>

					<button
						type="button"
						className="btn btn-outline btn-sm btn-error"
						onClick={handleRestartServer}
						disabled={restartServer.isPending}
						title="Restart the entire server"
					>
						{restartServer.isPending ? <LoadingSpinner size="sm" /> : <Radio className="h-4 w-4" />}
						Restart Server
					</button>
				</div>
			</div>

			{/* Restart Required Banner */}
			<RestartRequiredBanner
				restartRequiredConfigs={restartRequiredConfigs}
				onDismiss={handleDismissRestartBanner}
				isDismissed={isRestartBannerDismissed}
			/>

			{/* Library Sync Needed Banner */}
			{syncNeeded?.needs_sync && (
				<div className="alert alert-warning">
					<AlertTriangle className="h-6 w-6" />
					<div className="flex-1">
						<div className="font-bold">Library Sync Required</div>
						<div className="text-sm">
							Mount path has been updated. Run Library Sync to update existing symlinks.
						</div>
					</div>
					<button
						type="button"
						className="btn btn-primary btn-sm"
						onClick={() => triggerLibrarySync.mutate()}
						disabled={triggerLibrarySync.isPending}
					>
						{triggerLibrarySync.isPending ? (
							<LoadingSpinner size="sm" />
						) : (
							<RefreshCw className="h-4 w-4" />
						)}
						{triggerLibrarySync.isPending ? "Starting..." : "Run Library Sync"}
					</button>
				</div>
			)}

			{/* Success/Error Messages */}
			{reloadConfig.isSuccess && (
				<div className="alert alert-success">
					<Check className="h-6 w-6" />
					<div>Configuration reloaded successfully from file</div>
				</div>
			)}

			{reloadConfig.error && (
				<div className="alert alert-error">
					<X className="h-6 w-6" />
					<div>
						<div className="font-bold">Failed to reload configuration</div>
						<div className="text-sm">{reloadConfig.error.message}</div>
					</div>
				</div>
			)}

			{restartServer.isSuccess && (
				<div className="alert alert-info">
					<Radio className="h-6 w-6" />
					<div>Server restart initiated. Page will reload shortly...</div>
				</div>
			)}

			{restartServer.error && (
				<div className="alert alert-error">
					<X className="h-6 w-6" />
					<div>
						<div className="font-bold">Failed to restart server</div>
						<div className="text-sm">{restartServer.error.message}</div>
					</div>
				</div>
			)}

			{/* Main Content */}
			<div className="grid grid-cols-1 gap-6 lg:grid-cols-4">
				{/* Menu Navigation */}
				<div className="lg:col-span-1">
					<div className="card bg-base-100 shadow-lg">
						<div className="card-body p-4">
							<h3 className="mb-4 font-semibold">Configuration Sections</h3>
							<ul className="menu rounded-box bg-base-200">
								{sections.map(([key, section]) => {
									const IconComponent = getIconComponent(section.icon);
									return (
										<li key={key}>
											<button
												type="button"
												className={activeSection === key ? "active" : ""}
												onClick={() => navigate(`/config/${key}`)}
											>
												<IconComponent className="h-5 w-5" />
												<div className="min-w-0 flex-1">
													<div className="font-medium">{section.title}</div>
													<div className="truncate text-xs opacity-70">{section.description}</div>
												</div>
												{!section.canEdit && (
													<span className="badge badge-ghost badge-xs">Read Only</span>
												)}
											</button>
										</li>
									);
								})}
							</ul>
						</div>
					</div>
				</div>

				{/* Configuration Content */}
				<div className="lg:col-span-3">
					<div className="card bg-base-100 shadow-lg">
						<div className="card-body">
							{/* Section Header */}
							<div className="mb-6">
								<div className="mb-2 flex items-center space-x-3">
									{(() => {
										const IconComponent = getIconComponent(CONFIG_SECTIONS[activeSection].icon);
										return <IconComponent className="h-8 w-8 text-primary" />;
									})()}
									<div>
										<h2 className="font-bold text-2xl">{CONFIG_SECTIONS[activeSection].title}</h2>
										<p className="text-base-content/70">
											{CONFIG_SECTIONS[activeSection].description}
										</p>
									</div>
								</div>
							</div>

							{/* Configuration Form Content */}
							<div className="space-y-6">
								{activeSection === "webdav" && (
									<WebDAVConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "auth" && (
									<AuthConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "import" && (
									<ImportConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "metadata" && (
									<MetadataConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "streaming" && (
									<StreamingConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "system" && (
									<SystemConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										onRefresh={() => refetch().then(() => {})}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "providers" && <ProvidersConfigSection config={config} />}

								{activeSection === "rclone" && (
									<RCloneConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "sabnzbd" && (
									<SABnzbdConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "arrs" && (
									<ArrsConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "health" && (
									<HealthConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{/* Placeholder for other sections */}
								{![
									"webdav",
									"auth",
									"import",
									"metadata",
									"streaming",
									"system",
									"providers",
									"rclone",
									"sabnzbd",
									"arrs",
									"health",
								].includes(activeSection) && (
									<ComingSoonSection
										sectionName={CONFIG_SECTIONS[activeSection]?.title || activeSection}
									/>
								)}
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}
