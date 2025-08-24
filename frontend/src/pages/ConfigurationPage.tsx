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
import { useState } from "react";
import { ComingSoonSection } from "../components/config/ComingSoonSection";
import { ProvidersConfigSection } from "../components/config/ProvidersConfigSection";
import { StreamingConfigSection } from "../components/config/StreamingConfigSection";
import { SystemConfigSection } from "../components/config/SystemConfigSection";
import { WebDAVConfigSection } from "../components/config/WebDAVConfigSection";
import { ImportConfigSection } from "../components/config/WorkersConfigSection";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingSpinner } from "../components/ui/LoadingSpinner";
import {
	useConfig,
	useReloadConfig,
	useUpdateConfigSection,
} from "../hooks/useConfig";
import type {
	ConfigSection,
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
	const updateConfigSection = useUpdateConfigSection();
	const [activeSection, setActiveSection] = useState<ConfigSection | "system">(
		"webdav",
	);
	const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);

	// Handle configuration updates with save button
	const handleConfigUpdate = async (
		section: string,
		data: WebDAVConfig | StreamingConfig,
	) => {
		if (section === "webdav") {
			await updateConfigSection.mutateAsync({
				section: "webdav",
				config: { webdav: data as WebDAVConfig },
			});
		} else if (section === "streaming") {
			await updateConfigSection.mutateAsync({
				section: "streaming",
				config: { streaming: data as StreamingConfig },
			});
		}
	};

	const handleReloadConfig = async () => {
		try {
			await reloadConfig.mutateAsync();
			setHasUnsavedChanges(false);
		} catch (error) {
			console.error("Failed to reload configuration:", error);
		}
	};

	if (isLoading) {
		return (
			<div className="flex justify-center items-center min-h-[400px]">
				<LoadingSpinner size="lg" />
			</div>
		);
	}

	if (error) {
		return (
			<div className="space-y-4">
				<h1 className="text-3xl font-bold">Configuration</h1>
				<ErrorAlert error={error as Error} onRetry={() => refetch()} />
			</div>
		);
	}

	if (!config) {
		return (
			<div className="space-y-4">
				<h1 className="text-3xl font-bold">Configuration</h1>
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
						<h1 className="text-3xl font-bold">Configuration</h1>
						<p className="text-base-content/70">
							Manage system settings and preferences
						</p>
					</div>
				</div>

				<div className="flex items-center space-x-2">
					{hasUnsavedChanges && (
						<div className="badge badge-warning">
							<AlertTriangle className="h-4 w-4 mr-1" />
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
				</div>
			</div>

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

			{/* Main Content */}
			<div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
				{/* Menu Navigation */}
				<div className="lg:col-span-1">
					<div className="card bg-base-100 shadow-lg">
						<div className="card-body p-4">
							<h3 className="font-semibold mb-4">Configuration Sections</h3>
							<ul className="menu bg-base-200 rounded-box">
								{sections.map(([key, section]) => {
									const IconComponent = getIconComponent(section.icon);
									return (
										<li key={key}>
											<button
												type="button"
												className={activeSection === key ? "active" : ""}
												onClick={() => setActiveSection(key)}
											>
												<IconComponent className="h-5 w-5" />
												<div className="flex-1 min-w-0">
													<div className="font-medium">{section.title}</div>
													<div className="text-xs opacity-70 truncate">
														{section.description}
													</div>
												</div>
												{!section.canEdit && (
													<span className="badge badge-ghost badge-xs">
														Read Only
													</span>
												)}
												{section.requiresRestart && (
													<span className="badge badge-warning badge-xs">
														Restart
													</span>
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
								<div className="flex items-center space-x-3 mb-2">
									{(() => {
										const IconComponent = getIconComponent(
											CONFIG_SECTIONS[activeSection].icon,
										);
										return <IconComponent className="h-8 w-8 text-primary" />;
									})()}
									<div>
										<h2 className="text-2xl font-bold">
											{CONFIG_SECTIONS[activeSection].title}
										</h2>
										<p className="text-base-content/70">
											{CONFIG_SECTIONS[activeSection].description}
										</p>
									</div>
								</div>

								{/* Section Status */}
								<div className="flex items-center space-x-2">
									{CONFIG_SECTIONS[activeSection].requiresRestart && (
										<div className="badge badge-warning">
											<AlertTriangle className="h-3 w-3 mr-1" />
											Requires Restart
										</div>
									)}
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

								{activeSection === "import" && (
									<ImportConfigSection config={config} />
								)}

								{activeSection === "streaming" && (
									<StreamingConfigSection
										config={config}
										onUpdate={handleConfigUpdate}
										isUpdating={updateConfigSection.isPending}
									/>
								)}

								{activeSection === "system" && (
									<SystemConfigSection config={config} />
								)}

								{activeSection === "providers" && (
									<ProvidersConfigSection config={config} />
								)}

								{/* Placeholder for other sections */}
								{![
									"webdav",
									"import",
									"streaming",
									"system",
									"providers",
								].includes(activeSection) && (
									<ComingSoonSection
										sectionName={
											CONFIG_SECTIONS[activeSection]?.title || activeSection
										}
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
