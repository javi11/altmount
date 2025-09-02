import { AlertTriangle, Plus, Save } from "lucide-react";
import { useEffect, useState } from "react";
import ScraperInstanceCard from "./ScraperInstanceCard";
import type { ConfigResponse, ScraperConfig, ScraperInstanceConfig, ScraperType } from "../../types/config";

interface ScraperConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: ScraperConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

interface NewInstanceForm {
	name: string;
	type: ScraperType;
	url: string;
	api_key: string;
	enabled: boolean;
	scrape_interval_hours: number;
}

const DEFAULT_NEW_INSTANCE: NewInstanceForm = {
	name: "",
	type: "radarr",
	url: "",
	api_key: "",
	enabled: true,
	scrape_interval_hours: 24,
};

export function ScraperConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: ScraperConfigSectionProps) {
	const [formData, setFormData] = useState<ScraperConfig>(config.scraper);
	const [hasChanges, setHasChanges] = useState(false);
	const [showAddInstance, setShowAddInstance] = useState(false);
	const [newInstance, setNewInstance] = useState<NewInstanceForm>(DEFAULT_NEW_INSTANCE);
	const [validationErrors, setValidationErrors] = useState<string[]>([]);
	const [showApiKeys, setShowApiKeys] = useState<Record<string, boolean>>({});
	const [expandedInstances, setExpandedInstances] = useState<Record<string, boolean>>({});

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.scraper);
		setHasChanges(false);
		setValidationErrors([]);
	}, [config.scraper]);

	const validateForm = (data: ScraperConfig): string[] => {
		const errors: string[] = [];

		if (data.enabled) {
			// Validate default interval
			if (data.default_interval_hours <= 0) {
				errors.push("Default scrape interval must be greater than 0 hours");
			}

			// Validate mount path
			if (!data.mount_path.trim()) {
				errors.push("Mount path is required when scraper is enabled");
			} else if (!data.mount_path.startsWith("/")) {
				errors.push("Mount path must be an absolute path (start with /)");
			}

			// Validate instances
			const allInstanceNames = [
				...data.radarr_instances.map((i) => ({ name: i.name, type: "Radarr" })),
				...data.sonarr_instances.map((i) => ({ name: i.name, type: "Sonarr" })),
			];

			// Check for duplicate names
			const nameCount: Record<string, number> = {};
			allInstanceNames.forEach(({ name }) => {
				nameCount[name] = (nameCount[name] || 0) + 1;
			});

			Object.entries(nameCount).forEach(([name, count]) => {
				if (count > 1) {
					errors.push(`Instance name "${name}" is used multiple times`);
				}
			});

			// Validate individual instances
			[...data.radarr_instances, ...data.sonarr_instances].forEach((instance, index) => {
				const instanceType = data.radarr_instances.includes(instance) ? "Radarr" : "Sonarr";

				if (!instance.name.trim()) {
					errors.push(`${instanceType} instance #${index + 1}: Name is required`);
				}

				if (!instance.url.trim()) {
					errors.push(`${instanceType} instance "${instance.name}": URL is required`);
				} else {
					try {
						new URL(instance.url);
					} catch {
						errors.push(`${instanceType} instance "${instance.name}": Invalid URL format`);
					}
				}

				if (!instance.api_key.trim()) {
					errors.push(`${instanceType} instance "${instance.name}": API key is required`);
				}

				if (instance.scrape_interval_hours <= 0) {
					errors.push(
						`${instanceType} instance "${instance.name}": Scrape interval must be greater than 0 hours`,
					);
				}
			});
		}

		return errors;
	};

	const handleFormChange = (
		field: keyof ScraperConfig,
		value: ScraperConfig[keyof ScraperConfig],
	) => {
		const newFormData = { ...formData, [field]: value };
		setFormData(newFormData);
		setHasChanges(true);
		setValidationErrors(validateForm(newFormData));
	};

	const handleInstanceChange = (
		type: ScraperType,
		index: number,
		field: keyof ScraperInstanceConfig,
		value: ScraperInstanceConfig[keyof ScraperInstanceConfig],
	) => {
		const instancesKey = type === "radarr" ? "radarr_instances" : "sonarr_instances";
		const instances = [...formData[instancesKey]];
		instances[index] = { ...instances[index], [field]: value };

		const newFormData = { ...formData, [instancesKey]: instances };
		setFormData(newFormData);
		setHasChanges(true);
		setValidationErrors(validateForm(newFormData));
	};

	const removeInstance = (type: ScraperType, index: number) => {
		const instancesKey = type === "radarr" ? "radarr_instances" : "sonarr_instances";
		const instances = [...formData[instancesKey]];
		instances.splice(index, 1);

		const newFormData = { ...formData, [instancesKey]: instances };
		setFormData(newFormData);
		setHasChanges(true);
		setValidationErrors(validateForm(newFormData));
	};

	const addInstance = () => {
		if (!newInstance.name.trim() || !newInstance.url.trim() || !newInstance.api_key.trim()) {
			return;
		}

		const instancesKey = newInstance.type === "radarr" ? "radarr_instances" : "sonarr_instances";
		const instances = [
			...formData[instancesKey],
			{
				name: newInstance.name,
				url: newInstance.url,
				api_key: newInstance.api_key,
				enabled: newInstance.enabled,
				scrape_interval_hours: newInstance.scrape_interval_hours,
			},
		];

		const newFormData = { ...formData, [instancesKey]: instances };
		setFormData(newFormData);
		setHasChanges(true);
		setValidationErrors(validateForm(newFormData));

		// Reset form and hide
		setNewInstance(DEFAULT_NEW_INSTANCE);
		setShowAddInstance(false);
	};

	const handleSave = async () => {
		if (!onUpdate || validationErrors.length > 0) return;

		try {
			await onUpdate("scraper", formData);
			setHasChanges(false);
		} catch (error) {
			console.error("Failed to save scraper configuration:", error);
		}
	};

	const toggleApiKeyVisibility = (instanceId: string) => {
		setShowApiKeys((prev) => ({
			...prev,
			[instanceId]: !prev[instanceId],
		}));
	};

	const toggleInstanceExpanded = (instanceKey: string) => {
		setExpandedInstances((prev) => ({
			...prev,
			[instanceKey]: !prev[instanceKey],
		}));
	};

	const renderInstance = (instance: ScraperInstanceConfig, type: ScraperType, index: number) => {
		const instanceId = `${type}-${index}`; // Use index-based key for UI state
		const isApiKeyVisible = showApiKeys[instanceId] || false;
		const isExpanded = expandedInstances[instanceId] || false;

		return (
			<ScraperInstanceCard
				key={instanceId}
				instance={instance}
				type={type}
				index={index}
				isReadOnly={isReadOnly}
				isApiKeyVisible={isApiKeyVisible}
				isExpanded={isExpanded}
				onToggleApiKey={() => toggleApiKeyVisibility(instanceId)}
				onToggleExpanded={() => toggleInstanceExpanded(instanceId)}
				onRemove={() => removeInstance(type, index)}
				onInstanceChange={(field, value) => handleInstanceChange(type, index, field, value)}
			/>
		);
	};

	return (
		<div className="space-y-6">
			{/* Enable/Disable Scraper */}
			<div className="card bg-base-200">
				<div className="card-body">
					<div className="flex items-center justify-between">
						<div>
							<h3 className="font-semibold">Enable Scraper Service</h3>
							<p className="text-base-content/70 text-sm">
								Enable automatic scraping of Radarr and Sonarr instances for file indexing. This will enable file redownloading feature in case is corrupted.
							</p>
						</div>
						<input
							type="checkbox"
							className="checkbox checkbox-primary"
							checked={formData.enabled}
							onChange={(e) => handleFormChange("enabled", e.target.checked)}
							disabled={isReadOnly}
						/>
					</div>
				</div>
			</div>

			{/* Default Settings */}
			{formData.enabled && (
				<div className="card bg-base-200">
					<div className="card-body">
						<h3 className="mb-4 font-semibold">Default Settings</h3>

						<div className="space-y-4">
							<fieldset className="fieldset max-w-md">
								<legend className="fieldset-legend">Default Scrape Interval (hours)</legend>
								<input
									type="number"
									className="input"
									value={formData.default_interval_hours}
									onChange={(e) =>
										handleFormChange(
											"default_interval_hours",
											Number.parseInt(e.target.value, 10) || 24,
										)
									}
									min="1"
									max="168"
									disabled={isReadOnly}
								/>
								<p className="label">Default interval for new instances (1-168 hours)</p>
							</fieldset>

							<fieldset className="fieldset max-w-md">
								<legend className="fieldset-legend">WebDAV Mount Path</legend>
								<input
									type="text"
									className="input"
									value={formData.mount_path}
									onChange={(e) => handleFormChange("mount_path", e.target.value)}
									placeholder="/mnt/altmount"
									disabled={isReadOnly}
								/>
								<p className="label">
									Absolute path where WebDAV is mounted. In case you have a setup an union in the arrs, add the union instead. Ex: "/mnt/unionfs", "/mnt/altmount"
								</p>
							</fieldset>
						</div>
					</div>
				</div>
			)}

			{/* Radarr Instances */}
			{formData.enabled && (
				<div className="card bg-base-100">
					<div className="card-body">
						<div className="mb-4 flex items-center justify-between">
							<h3 className="font-semibold">Radarr Instances</h3>
							<button
								type="button"
								className="btn btn-sm btn-primary"
								onClick={() => {
									setNewInstance({ ...DEFAULT_NEW_INSTANCE, type: "radarr" });
									setShowAddInstance(true);
								}}
								disabled={isReadOnly}
							>
								<Plus className="h-4 w-4" />
								Add Radarr
							</button>
						</div>

						{formData.radarr_instances.length === 0 && (
							<div className="py-8 text-center text-base-content/70">
								<p>No Radarr instances configured</p>
							</div>
						)}

						<div className="space-y-4">
							{formData.radarr_instances.map((instance, index) =>
								renderInstance(instance, "radarr", index),
							)}
						</div>
					</div>
				</div>
			)}

			{/* Sonarr Instances */}
			{formData.enabled && (
				<div className="card bg-base-100">
					<div className="card-body">
						<div className="mb-4 flex items-center justify-between">
							<h3 className="font-semibold">Sonarr Instances</h3>
							<button
								type="button"
								className="btn btn-sm btn-primary"
								onClick={() => {
									setNewInstance({ ...DEFAULT_NEW_INSTANCE, type: "sonarr" });
									setShowAddInstance(true);
								}}
								disabled={isReadOnly}
							>
								<Plus className="h-4 w-4" />
								Add Sonarr
							</button>
						</div>

						{formData.sonarr_instances.length === 0 && (
							<div className="py-8 text-center text-base-content/70">
								<p>No Sonarr instances configured</p>
							</div>
						)}

						<div className="space-y-4">
							{formData.sonarr_instances.map((instance, index) =>
								renderInstance(instance, "sonarr", index),
							)}
						</div>
					</div>
				</div>
			)}

			{/* Add Instance Modal */}
			{showAddInstance && (
				<div className="modal modal-open">
					<div className="modal-box">
						<h3 className="mb-4 font-bold text-lg">
							Add {newInstance.type === "radarr" ? "Radarr" : "Sonarr"} Instance
						</h3>

						<div className="space-y-4">
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Instance Name</legend>
								<input
									type="text"
									className="input"
									value={newInstance.name}
									onChange={(e) => setNewInstance((prev) => ({ ...prev, name: e.target.value }))}
									placeholder="My Radarr/Sonarr"
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">URL</legend>
								<input
									type="url"
									className="input"
									value={newInstance.url}
									onChange={(e) => setNewInstance((prev) => ({ ...prev, url: e.target.value }))}
									placeholder="http://localhost:7878"
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">API Key</legend>
								<input
									type="password"
									className="input"
									value={newInstance.api_key}
									onChange={(e) => setNewInstance((prev) => ({ ...prev, api_key: e.target.value }))}
									placeholder="API key from settings"
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Scrape Interval (hours)</legend>
								<input
									type="number"
									className="input"
									value={newInstance.scrape_interval_hours}
									onChange={(e) =>
										setNewInstance((prev) => ({
											...prev,
											scrape_interval_hours: Number.parseInt(e.target.value, 10) || 24,
										}))
									}
									min="1"
									max="168"
								/>
							</fieldset>

							<label className="label cursor-pointer">
								<span className="label-text">Enable this instance</span>
								<input
									type="checkbox"
									className="checkbox"
									checked={newInstance.enabled}
									onChange={(e) =>
										setNewInstance((prev) => ({ ...prev, enabled: e.target.checked }))
									}
								/>
							</label>
						</div>

						<div className="modal-action">
							<button
								type="button"
								className="btn btn-ghost"
								onClick={() => {
									setShowAddInstance(false);
									setNewInstance(DEFAULT_NEW_INSTANCE);
								}}
							>
								Cancel
							</button>
							<button
								type="button"
								className="btn btn-primary"
								onClick={addInstance}
								disabled={
									!newInstance.name.trim() || !newInstance.url.trim() || !newInstance.api_key.trim()
								}
							>
								<Plus className="h-4 w-4" />
								Add Instance
							</button>
						</div>
					</div>
				</div>
			)}

			{/* Validation Errors */}
			{validationErrors.length > 0 && (
				<div className="alert alert-warning">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">Configuration Issues</div>
						<ul className="mt-2 space-y-1">
							{validationErrors.map((error, index) => (
								<li key={index} className="text-sm">
									• {error}
								</li>
							))}
						</ul>
					</div>
				</div>
			)}

			{/* Save Button */}
			{hasChanges && onUpdate && (
				<div className="flex justify-end border-base-300 border-t pt-4">
					<button
						type="button"
						className="btn btn-primary"
						onClick={handleSave}
						disabled={isUpdating || validationErrors.length > 0}
					>
						{isUpdating ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<Save className="h-4 w-4" />
						)}
						{isUpdating ? "Saving..." : "Save Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
