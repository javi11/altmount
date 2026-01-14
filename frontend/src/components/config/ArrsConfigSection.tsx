import { AlertTriangle, Plus, Save, Trash2, Webhook } from "lucide-react";
import { useEffect, useState } from "react";
import { useRegisterArrsWebhooks } from "../../hooks/useApi";
import type { ArrsConfig, ArrsInstanceConfig, ArrsType, ConfigResponse } from "../../types/config";
import ArrsInstanceCard from "./ArrsInstanceCard";

interface ArrsConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: ArrsConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

interface NewInstanceForm {
	name: string;
	type: ArrsType;
	url: string;
	api_key: string;
	category: string;
	enabled: boolean;
}

const DEFAULT_NEW_INSTANCE: NewInstanceForm = {
	name: "",
	type: "radarr",
	url: "",
	api_key: "",
	category: "movies",
	enabled: true,
};

export function ArrsConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: ArrsConfigSectionProps) {
	const [formData, setFormData] = useState<ArrsConfig>(config.arrs);
	const [hasChanges, setHasChanges] = useState(false);
	const [showAddInstance, setShowAddInstance] = useState(false);
	const [newInstance, setNewInstance] = useState<NewInstanceForm>(DEFAULT_NEW_INSTANCE);
	const [validationErrors, setValidationErrors] = useState<string[]>([]);
	const [showApiKeys, setShowApiKeys] = useState<Record<string, boolean>>({});
	const [webhookSuccess, setWebhookSuccess] = useState<string | null>(null);
	const [webhookError, setWebhookError] = useState<string | null>(null);
	const [saveError, setSaveError] = useState<string | null>(null);
	const [newIgnoreMessage, setNewIgnoreMessage] = useState("");

	const registerWebhooks = useRegisterArrsWebhooks();

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.arrs);
		setHasChanges(false);
		setValidationErrors([]);
		setSaveError(null);
	}, [config.arrs]);

	const handleRegisterWebhooks = async () => {
		setWebhookSuccess(null);
		setWebhookError(null);
		try {
			await registerWebhooks.mutateAsync();
			setWebhookSuccess("Webhook registration triggered successfully.");
			// Hide success message after 5 seconds
			setTimeout(() => setWebhookSuccess(null), 5000);
		} catch (error) {
			setWebhookError(error instanceof Error ? error.message : "Failed to register webhooks.");
		}
	};

	const validateForm = (data: ArrsConfig): string[] => {
		const errors: string[] = [];

		if (data.enabled) {
			// Validate mount_path is configured
			if (!config.mount_path) {
				errors.push(
					"Mount Path must be configured in General/System settings before enabling Arrs service",
				);
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
			});
		}

		return errors;
	};

	const handleFormChange = (field: keyof ArrsConfig, value: ArrsConfig[keyof ArrsConfig]) => {
		const newFormData = { ...formData, [field]: value };
		setFormData(newFormData);
		setHasChanges(true);
		setValidationErrors(validateForm(newFormData));
	};

	const handleInstanceChange = (
		type: ArrsType,
		index: number,
		field: keyof ArrsInstanceConfig,
		value: ArrsInstanceConfig[keyof ArrsInstanceConfig],
	) => {
		const instancesKey = type === "radarr" ? "radarr_instances" : "sonarr_instances";
		const instances = [...formData[instancesKey]];
		instances[index] = { ...instances[index], [field]: value };

		const newFormData = { ...formData, [instancesKey]: instances };
		setFormData(newFormData);
		setHasChanges(true);
		setValidationErrors(validateForm(newFormData));
	};

	const removeInstance = (type: ArrsType, index: number) => {
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

		// Use provided category or default based on type
		let category = newInstance.category.trim();
		if (!category) {
			category = newInstance.type === "radarr" ? "movies" : "tv";
		}

		const instances = [
			...formData[instancesKey],
			{
				name: newInstance.name,
				url: newInstance.url,
				api_key: newInstance.api_key,
				category: category,
				enabled: newInstance.enabled,
				sync_interval_hours: 1,
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

	const handleAddIgnoreMessage = () => {
		if (!newIgnoreMessage.trim()) return;

		const currentList = formData.queue_cleanup_allowlist || [];
		if (currentList.some((m) => m.message === newIgnoreMessage.trim())) {
			setNewIgnoreMessage("");
			return;
		}

		const newList = [...currentList, { message: newIgnoreMessage.trim(), enabled: true }];
		handleFormChange("queue_cleanup_allowlist", newList);
		setNewIgnoreMessage("");
	};

	const handleRemoveIgnoreMessage = (index: number) => {
		const currentList = formData.queue_cleanup_allowlist || [];
		const newList = [...currentList];
		newList.splice(index, 1);
		handleFormChange("queue_cleanup_allowlist", newList);
	};

	const handleToggleIgnoreMessage = (index: number) => {
		const currentList = formData.queue_cleanup_allowlist || [];
		const newList = [...currentList];
		newList[index] = { ...newList[index], enabled: !newList[index].enabled };
		handleFormChange("queue_cleanup_allowlist", newList);
	};

	const handleSave = async () => {
		if (!onUpdate || validationErrors.length > 0) return;
		setSaveError(null);

		try {
			await onUpdate("arrs", formData);
			setHasChanges(false);
		} catch (error) {
			console.error("Failed to save arrs configuration:", error);
			setSaveError(error instanceof Error ? error.message : "Failed to save configuration");
		}
	};

	const toggleApiKeyVisibility = (instanceId: string) => {
		setShowApiKeys((prev) => ({
			...prev,
			[instanceId]: !prev[instanceId],
		}));
	};

	const renderInstance = (instance: ArrsInstanceConfig, type: ArrsType, index: number) => {
		const instanceId = `${type}-${index}`; // Use index-based key for UI state
		const isApiKeyVisible = showApiKeys[instanceId] || false;

		return (
			<ArrsInstanceCard
				key={instanceId}
				instance={instance}
				type={type}
				index={index}
				isReadOnly={isReadOnly}
				isApiKeyVisible={isApiKeyVisible}
				categories={config.sabnzbd?.categories}
				onToggleApiKey={() => toggleApiKeyVisibility(instanceId)}
				onRemove={() => removeInstance(type, index)}
				onInstanceChange={(field, value) => handleInstanceChange(type, index, field, value)}
			/>
		);
	};

	return (
		<div className="space-y-6">
			{/* Enable/Disable Arrs */}
			<div className="card bg-base-200">
				<div className="card-body">
					<div className="flex items-center justify-between">
						<div>
							<h3 className="font-semibold">Enable Arrs Service</h3>
							<p className="text-base-content/70 text-sm">
								Enable health monitoring and file repair for Radarr and Sonarr instances. This
								allows automatic detection and repair of corrupted files.
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

			{/* Webhooks Auto-Registration */}
			{formData.enabled && (
				<div className="card bg-base-200">
					<div className="card-body">
						<div className="flex flex-col space-y-4">
							<div>
								<h3 className="font-semibold">Connect Webhooks</h3>
								<p className="text-base-content/70 text-sm">
									Automatically configure AltMount webhooks in all enabled Radarr and Sonarr
									instances. This ensures AltMount is notified when files are upgraded or renamed.
								</p>
							</div>

							<div className="flex flex-col space-y-4 md:flex-row md:items-end md:space-x-4 md:space-y-0">
								<fieldset className="fieldset flex-1">
									<legend className="fieldset-legend">AltMount URL (for webhooks)</legend>
									<input
										type="url"
										className="input w-full"
										value={formData.webhook_base_url ?? "http://altmount:8080"}
										onChange={(e) => handleFormChange("webhook_base_url", e.target.value)}
										placeholder="http://altmount:8080"
										disabled={isReadOnly}
									/>
									<p className="label text-base-content/70 text-xs">
										The URL ARR instances will use to talk back to AltMount.
									</p>
								</fieldset>

								<button
									type="button"
									className="btn btn-primary"
									onClick={handleRegisterWebhooks}
									disabled={isReadOnly || registerWebhooks.isPending || hasChanges}
									title={hasChanges ? "Save changes before registering webhooks" : ""}
								>
									{registerWebhooks.isPending ? (
										<span className="loading loading-spinner loading-sm" />
									) : (
										<Webhook className="h-4 w-4" />
									)}
									Auto-Setup ARR Webhooks
								</button>
							</div>
						</div>
						{webhookSuccess && (
							<div className="alert alert-success mt-4 py-2">{webhookSuccess}</div>
						)}
						{webhookError && <div className="alert alert-error mt-4 py-2">{webhookError}</div>}
					</div>
				</div>
			)}

			{/* Queue Cleanup Settings */}
			{formData.enabled && (
				<div className="card bg-base-200">
					<div className="card-body">
						<h3 className="mb-4 font-semibold">Queue Cleanup</h3>
						<p className="mb-4 text-base-content/70 text-sm">
							Automatically clean up empty folders from import pending items in Radarr/Sonarr
							queues. This removes stale queue entries where the download folder no longer contains
							any files.
						</p>

						<div className="space-y-4">
							<div className="flex items-center justify-between">
								<div>
									<span className="font-medium">Enable Queue Cleanup</span>
									<p className="text-base-content/70 text-sm">
										Periodically check for and remove empty import pending folders
									</p>
								</div>
								<input
									type="checkbox"
									className="checkbox checkbox-primary"
									checked={formData.queue_cleanup_enabled ?? true}
									onChange={(e) => handleFormChange("queue_cleanup_enabled", e.target.checked)}
									disabled={isReadOnly}
								/>
							</div>

							{(formData.queue_cleanup_enabled ?? true) && (
								<>
									<fieldset className="fieldset">
										<legend className="fieldset-legend">Cleanup Interval (seconds)</legend>
										<input
											type="number"
											className="input w-full max-w-xs"
											value={formData.queue_cleanup_interval_seconds ?? 10}
											onChange={(e) =>
												handleFormChange(
													"queue_cleanup_interval_seconds",
													Number.parseInt(e.target.value, 10) || 10,
												)
											}
											min={1}
											max={3600}
											disabled={isReadOnly}
										/>
										<p className="label text-base-content/70">
											How often to check for empty import pending folders (default: 10 seconds)
										</p>
									</fieldset>

									<div className="divider" />

									<div>
										<h4 className="font-medium mb-2">Ignored Error Messages</h4>
										<p className="text-base-content/70 text-sm mb-4">
											Additional error messages that are safe to auto-cleanup. You can enable/disable default rules or add custom ones.
										</p>

										{/* List of ignored messages */}
										<div className="space-y-2 mb-4">
											{(formData.queue_cleanup_allowlist || []).map((msg, index) => (
												<div
													key={index}
													className="flex items-center justify-between bg-base-300 p-2 rounded-lg"
												>
													<div className="flex items-center flex-1 mr-4">
														<input
															type="checkbox"
															className="checkbox checkbox-sm checkbox-primary mr-3"
															checked={msg.enabled}
															onChange={() => handleToggleIgnoreMessage(index)}
															disabled={isReadOnly}
														/>
														<span className={`text-sm font-mono break-all ${!msg.enabled ? "opacity-50 line-through" : ""}`}>
															{msg.message}
														</span>
													</div>
													<button
														type="button"
														className="btn btn-ghost btn-xs btn-circle text-error"
														onClick={() => handleRemoveIgnoreMessage(index)}
														disabled={isReadOnly}
														title="Delete rule"
													>
														<Trash2 className="h-4 w-4" />
													</button>
												</div>
											))}
											{(formData.queue_cleanup_allowlist || []).length === 0 && (
												<div className="text-center py-4 bg-base-100 rounded-lg text-base-content/50 text-sm italic">
													No ignored messages configured
												</div>
											)}
										</div>

										{/* Add new message input */}
										<div className="flex gap-2">
											<input
												type="text"
												className="input input-sm w-full"
												placeholder="e.g. Not a Custom Format upgrade"
												value={newIgnoreMessage}
												onChange={(e) => setNewIgnoreMessage(e.target.value)}
												onKeyDown={(e) => {
													if (e.key === "Enter") {
														e.preventDefault();
														handleAddIgnoreMessage();
													}
												}}
												disabled={isReadOnly}
											/>
											<button
												type="button"
												className="btn btn-sm btn-primary"
												onClick={handleAddIgnoreMessage}
												disabled={isReadOnly || !newIgnoreMessage.trim()}
											>
												<Plus className="h-4 w-4" />
												Add
											</button>
										</div>
									</div>
								</>
							)}
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
									setNewInstance({ ...DEFAULT_NEW_INSTANCE, type: "sonarr", category: "tv" });
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
								<legend className="fieldset-legend">Download Category (Optional)</legend>
								<select
									className="select"
									value={newInstance.category}
									onChange={(e) =>
										setNewInstance((prev) => ({ ...prev, category: e.target.value }))
									}
								>
									<option value="">None</option>
									{config.sabnzbd?.categories?.map((cat) => (
										<option key={cat.name} value={cat.name}>
											{cat.name}
										</option>
									))}
								</select>
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

			{/* Save Error */}
			{saveError && (
				<div className="alert alert-error">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">Save Failed</div>
						<div className="text-sm">{saveError}</div>
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
