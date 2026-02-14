import {
	AlertTriangle,
	Cog,
	Download,
	Folder,
	Globe,
	HardDrive,
	Radio,
	RefreshCw,
	Settings,
	Shield,
	ShieldAlert,
	Activity,
	Wrench,
	Server,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ArrsConfigSection } from "../components/config/ArrsConfigSection";
import { AuthConfigSection } from "../components/config/AuthConfigSection";
import { ComingSoonSection } from "../components/config/ComingSoonSection";
import { HealthConfigSection } from "../components/config/HealthConfigSection";
import { MetadataConfigSection } from "../components/config/MetadataConfigSection";
import { MountConfigSection } from "../components/config/MountConfigSection";
import { ProvidersConfigSection } from "../components/config/ProvidersConfigSection";
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
	ProviderConfig,
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
		ShieldAlert,
		Activity,
		Wrench,
		Server,
	};
	return iconMap[iconName as keyof typeof iconMap] || Settings;
};

// Define section groups for modern organization
const SECTION_GROUPS = [
	{
		title: "Core Services",
		sections: ["webdav", "mount", "providers"],
	},
	{
		title: "Media Management",
		sections: ["metadata", "streaming"],
	},
	{
		title: "Automation",
		sections: ["sabnzbd", "arrs", "health"],
	},
	{
		title: "System",
		sections: ["auth", "import", "system"],
	},
];

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
		// Redirect legacy rclone/fuse paths to mount
		if (section === "rclone" || section === "fuse") return "mount" as ConfigSection;
		const validSections = Object.keys(CONFIG_SECTIONS) as (ConfigSection | "system")[];
		return validSections.includes(section as ConfigSection | "system")
			? (section as ConfigSection | "system")
			: "webdav";
	})();

	// Redirect to default section if no section is specified, or legacy paths
	useEffect(() => {
		if (!section) {
			navigate("/config/webdav", { replace: true });
		} else if (section === "rclone" || section === "fuse") {
			navigate("/config/mount", { replace: true });
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

	// Handle configuration updates
	// biome-ignore lint/suspicious/noExplicitAny: accepts various config types
	const handleConfigUpdate = async (section: string, data: any) => {
		try {
			if (section === "webdav" && config) {
				const webdavData = data as unknown as WebDAVConfig;
				const portChanged = webdavData.port !== config.webdav.port;
				await updateConfigSection.mutateAsync({
					section: "webdav",
					config: { webdav: webdavData },
				});
				if (portChanged) addRestartRequiredConfig("WebDAV Port");
			} else if (section === "auth") {
				await updateConfigSection.mutateAsync({
					section: "auth",
					config: { auth: data as unknown as AuthConfig },
				});
			} else if (section === "streaming") {
				await updateConfigSection.mutateAsync({
					section: "streaming",
					config: { streaming: data as unknown as StreamingConfig },
				});
			} else if (section === "import" && config) {
				const importData = data as unknown as ImportConfig;
				const workersChanged =
					importData.max_processor_workers !== config.import.max_processor_workers;
				await updateConfigSection.mutateAsync({
					section: "import",
					config: { import: importData },
				});
				if (workersChanged) addRestartRequiredConfig("Import Max Processor Workers");
			} else if (section === "metadata" && config) {
				const metadataData = data as unknown as MetadataConfig;
				const rootPathChanged = metadataData.root_path !== config.metadata.root_path;
				await updateConfigSection.mutateAsync({
					section: "metadata",
					config: { metadata: metadataData },
				});
				if (rootPathChanged) addRestartRequiredConfig("Metadata Root Path");
			} else if (section === "mount") {
				await updateConfigSection.mutateAsync({
					section: "mount",
					config: data,
				});
			} else if (section === "sabnzbd") {
				await updateConfigSection.mutateAsync({
					section: "sabnzbd",
					config: { sabnzbd: data as unknown as SABnzbdConfig },
				});
			} else if (section === "arrs") {
				await updateConfigSection.mutateAsync({
					section: "arrs",
					config: { arrs: data as unknown as ArrsConfig },
				});
			} else if (section === "health") {
				await updateConfigSection.mutateAsync({
					section: "health",
					config: { health: data as unknown as HealthConfig },
				});
			} else if (section === "providers") {
				await updateConfigSection.mutateAsync({
					section: "providers",
					config: { providers: data as unknown as ProviderConfig[] },
				});
			} else if (section === "log") {
				await updateConfigSection.mutateAsync({
					section: "system",
					config: { log: data as unknown as LogFormData },
				});
			}
		} catch (error) {
			console.error("Failed to update configuration:", error);
			throw error;
		}
	};

	if (isLoading) {
		return (
			<div className="flex h-[60vh] w-full items-center justify-center">
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
						<div className="text-sm">Unable to load configuration.</div>
					</div>
				</div>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
				<div className="flex items-center space-x-3">
					<div className="rounded-xl bg-primary/10 p-2 shadow-inner">
						<Settings className="h-8 w-8 text-primary" />
					</div>
					<div>
						<h1 className="font-bold text-3xl tracking-tight">Configuration</h1>
						<p className="text-base-content/60 text-sm">System settings and preferences.</p>
					</div>
				</div>

				<div className="flex items-center gap-2">
					{hasUnsavedChanges && (
						<div className="badge badge-warning badge-sm gap-1 py-3 font-bold animate-pulse">
							<AlertTriangle className="h-3 w-3" /> UNSAVED
						</div>
					)}

					<button
						type="button"
						className="btn btn-ghost btn-sm border-base-300 bg-base-100 hover:bg-base-200"
						onClick={handleReloadConfig}
						disabled={reloadConfig.isPending}
					>
						{reloadConfig.isPending ? <LoadingSpinner size="sm" /> : <RefreshCw className="h-4 w-4" />}
						Reload
					</button>

					<button
						type="button"
						className="btn btn-error btn-outline btn-sm"
						onClick={handleRestartServer}
						disabled={restartServer.isPending}
					>
						{restartServer.isPending ? <LoadingSpinner size="sm" /> : <Radio className="h-4 w-4" />}
						Restart
					</button>
				</div>
			</div>

			<RestartRequiredBanner
				restartRequiredConfigs={restartRequiredConfigs}
				onDismiss={handleDismissRestartBanner}
				isDismissed={isRestartBannerDismissed}
			/>

			{syncNeeded?.needs_sync && (
				<div className="alert alert-warning rounded-2xl border border-warning/20 bg-warning/5 shadow-sm">
					<AlertTriangle className="h-6 w-6" />
					<div className="flex-1">
						<div className="font-bold">Library Sync Required</div>
						<div className="text-sm opacity-80">Mount path has been updated. Update symlinks?</div>
					</div>
					<button
						type="button"
						className="btn btn-primary btn-sm px-6"
						onClick={() => triggerLibrarySync.mutate()}
						disabled={triggerLibrarySync.isPending}
					>
						{triggerLibrarySync.isPending ? <LoadingSpinner size="sm" /> : "Run Now"}
					</button>
				</div>
			)}

			<div className="grid grid-cols-1 gap-6 lg:grid-cols-4">
				{/* Modern Sidebar (Stacks on mobile, exactly like Import) */}
				<div className="lg:col-span-1">
					<div className="card border border-base-200 bg-base-100/50 shadow-sm backdrop-blur-md sticky top-24">
						<div className="card-body p-2 sm:p-4">
							{SECTION_GROUPS.map((group) => (
								<div key={group.title} className="mb-4 last:mb-0">
									<h3 className="mb-2 px-4 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
										{group.title}
									</h3>
									<ul className="menu menu-md gap-1 p-0">
										{group.sections.map((key) => {
											const s = CONFIG_SECTIONS[key as ConfigSection | "system"];
											if (s.hidden) return null;
											const Icon = getIconComponent(s.icon);
											const isActive = activeSection === key;
											return (
												<li key={key}>
													<button
														type="button"
														className={`flex items-center gap-3 rounded-xl px-4 py-3 transition-all ${
															isActive
																? "bg-primary font-bold text-primary-content shadow-lg shadow-primary/20 scale-[1.02]"
																: "hover:bg-base-200 text-base-content/70"
														}`}
														onClick={() => navigate(`/config/${key}`)}
													>
														<Icon className={`h-5 w-5 ${isActive ? "" : "text-base-content/40"}`} />
														<div className="min-w-0 flex-1 text-left">
															<div className="text-sm">{s.title}</div>
														</div>
														{!s.canEdit && <span className="badge badge-ghost badge-xs opacity-50">🔒</span>}
													</button>
												</li>
											);
										})}
									</ul>
								</div>
							))}
						</div>
					</div>
				</div>

				{/* Modern Content Card */}
				<div className="lg:col-span-3">
					<div className="card min-h-[600px] border border-base-200 bg-base-100 shadow-sm overflow-hidden rounded-2xl">
						<div className="card-body p-4 sm:p-10">
							{/* Modern Section Header */}
							<div className="mb-10 border-base-200 border-b pb-8">
								<div className="mb-2 flex items-start sm:items-center space-x-5">
									<div className="rounded-2xl bg-primary/10 p-4 shrink-0 shadow-inner">
										{(() => {
											const Icon = getIconComponent(CONFIG_SECTIONS[activeSection].icon);
											return <Icon className="h-8 w-8 text-primary" />;
										})()}
									</div>
									<div className="min-w-0 flex-1">
										<h2 className="font-bold text-3xl tracking-tight break-words text-base-content">
											{CONFIG_SECTIONS[activeSection].title}
										</h2>
										<p className="max-w-2xl text-base-content/60 text-sm leading-relaxed break-words mt-1">
											{CONFIG_SECTIONS[activeSection].description}
										</p>
									</div>
								</div>
							</div>

							<div className="max-w-4xl mx-auto w-full">
								{activeSection === "webdav" && (
									<WebDAVConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "auth" && (
									<AuthConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "import" && (
									<ImportConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "metadata" && (
									<MetadataConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "streaming" && (
									<StreamingConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "system" && (
									<SystemConfigSection config={config} onUpdate={handleConfigUpdate} onRefresh={async () => { await refetch(); }} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "providers" && (
									<ProvidersConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "mount" && (
									<MountConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "sabnzbd" && (
									<SABnzbdConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "arrs" && (
									<ArrsConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{activeSection === "health" && (
									<HealthConfigSection config={config} onUpdate={handleConfigUpdate} isUpdating={updateConfigSection.isPending} />
								)}
								{![ "webdav", "auth", "import", "metadata", "streaming", "system", "providers", "mount", "sabnzbd", "arrs", "health" ].includes(activeSection) && (
									<ComingSoonSection sectionName={CONFIG_SECTIONS[activeSection]?.title || activeSection} />
								)}
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}
