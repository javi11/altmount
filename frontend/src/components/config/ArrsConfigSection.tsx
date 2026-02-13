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
	const [newIgnoreMessage, setNewIgnoreMessage] = useState("");

	const registerWebhooks = useRegisterArrsWebhooks();

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.arrs);
		setHasChanges(false);
		setValidationErrors([]);
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

		try {
			await onUpdate("arrs", formData);
			setHasChanges(false);
		} catch (error) {
			console.error("Failed to save arrs configuration:", error);
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
		<div className="space-y-10">
			{/* Enable/Disable Arrs */}
			<section className="space-y-4">
				<div className="mb-2 flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Service Status</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>
				
				<div className="card border border-base-300 bg-base-200/50 shadow-sm">
					<div className="card-body p-4 sm:p-6">
						<div className="flex items-center justify-between gap-4">
							<div className="flex-1">
								<h3 className="font-bold text-sm sm:text-base">Enable Arrs Automation</h3>
								<p className="mt-1 text-base-content/60 text-xs leading-relaxed">
									Connect Radarr and Sonarr for automatic health monitoring and file repair.
								</p>
							</div>
							<input
								type="checkbox"
								className="toggle toggle-primary"
								checked={formData.enabled}
								onChange={(e) => handleFormChange("enabled", e.target.checked)}
								disabled={isReadOnly}
							/>
						</div>
					</div>
				</div>
			</section>

			{formData.enabled && (
				<div className="fade-in animate-in space-y-10 duration-500">
					{/* Connectivity Settings */}
					<section className="space-y-4">
						<div className="mb-2 flex items-center gap-2">
							<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Connectivity</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">AltMount Webhook URL</legend>
								<input
									type="url"
									className="input w-full bg-base-200/50"
									value={formData.webhook_base_url ?? "http://altmount:8080"}
									onChange={(e) => handleFormChange("webhook_base_url", e.target.value)}
									placeholder="http://altmount:8080"
									disabled={isReadOnly}
								/>
								<p className="label mt-1 text-[10px] leading-relaxed opacity-60">
									URL used by ARR instances to notify AltMount of changes.
								</p>
							</fieldset>

							<div className="flex flex-col justify-end pb-1">
								<button
									type="button"
									className="btn btn-outline btn-sm w-full"
									onClick={handleRegisterWebhooks}
									disabled={isReadOnly || registerWebhooks.isPending || hasChanges}
								>
									{registerWebhooks.isPending ? (
										<span className="loading loading-spinner loading-xs" />
									) : (
										<Webhook className="h-3.5 w-3.5" />
									)}
									Auto-Setup ARR Webhooks
								</button>
							</div>
						</div>
						
						{webhookSuccess && <div className="alert alert-success py-2 text-xs shadow-sm">{webhookSuccess}</div>}
						{webhookError && <div className="alert alert-error py-2 text-xs shadow-sm">{webhookError}</div>}
					</section>

					{/* Queue Maintenance Section */}
					<section className="space-y-4">
						<div className="mb-2 flex items-center gap-2">
							<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Queue Maintenance</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="grid grid-cols-1 gap-8 md:grid-cols-2">
							<div className="space-y-6">
								<div className="space-y-3">
									<label className="label cursor-pointer justify-start gap-3 py-0">
										<input type="checkbox" className="checkbox checkbox-sm checkbox-primary" checked={formData.queue_cleanup_enabled ?? true} onChange={(e) => handleFormChange("queue_cleanup_enabled", e.target.checked)} disabled={isReadOnly} />
										<span className="label-text font-medium text-xs">Enable Automatic Cleanup</span>
									</label>
									<label className="label cursor-pointer justify-start gap-3 py-0">
										<input type="checkbox" className="checkbox checkbox-sm checkbox-primary" checked={formData.cleanup_automatic_import_failure ?? false} onChange={(e) => handleFormChange("cleanup_automatic_import_failure", e.target.checked)} disabled={isReadOnly} />
										<span className="label-text font-medium text-xs">Cleanup failed imports</span>
									</label>
								</div>

																<div className="grid grid-cols-2 gap-4 lg:grid-cols-3">

																	<div className="space-y-1">

																		<span className="font-bold text-[10px] uppercase opacity-50">Interval (s)</span>

																		<input

																			type="number"

																			className="input input-xs input-bordered w-full font-mono"

																			value={formData.queue_cleanup_interval_seconds ?? 10}

																			onChange={(e) => handleFormChange("queue_cleanup_interval_seconds", Number.parseInt(e.target.value, 10) || 10)}

																		/>

																	</div>

																	<div className="space-y-1">

																		<span className="font-bold text-[10px] uppercase opacity-50">Grace Period (m)</span>

																		<input

																			type="number"

																			className="input input-xs input-bordered w-full font-mono"

																			value={formData.queue_cleanup_grace_period_minutes ?? 10}

																			onChange={(e) => handleFormChange("queue_cleanup_grace_period_minutes", Number.parseInt(e.target.value, 10) || 10)}

																		/>

																	</div>

																	<div className="col-span-2 space-y-1 lg:col-span-1">

																		<span className="font-bold text-[10px] uppercase opacity-50">Max Workers</span>

																		<input

																			type="number"

																			className="input input-xs input-bordered w-full font-mono"

																			value={formData.max_workers ?? 1}

																			onChange={(e) => handleFormChange("max_workers", Number.parseInt(e.target.value, 10) || 1)}

																		/>

																	</div>

																</div>
							</div>

							<div className="space-y-4">
								<h5 className="border-base-200 border-b pb-1 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">Ignored Error Rules</h5>
								<div className="custom-scrollbar max-h-40 space-y-2 overflow-y-auto pr-2">
									{(formData.queue_cleanup_allowlist || []).map((msg, index) => (
										<div key={index} className="flex items-center justify-between gap-2 rounded-lg bg-base-200 p-2">
											<div className="flex min-w-0 flex-1 items-center gap-2">
												<input type="checkbox" className="checkbox checkbox-xs" checked={msg.enabled} onChange={() => handleToggleIgnoreMessage(index)} />
												<span className={`truncate font-mono text-[10px] ${!msg.enabled ? "line-through opacity-40" : ""}`}>{msg.message}</span>
											</div>
											<button type="button" className="btn btn-ghost btn-xs px-1" onClick={() => handleRemoveIgnoreMessage(index)}><Trash2 className="h-3 w-3 text-error" /></button>
										</div>
									))}
								</div>
								<div className="flex gap-2">
									<input
										type="text"
										className="input input-xs input-bordered flex-1"
										placeholder="Add rule..."
										value={newIgnoreMessage}
										onChange={(e) => setNewIgnoreMessage(e.target.value)}
										onKeyDown={(e) => {
											if (e.key === "Enter") {
												e.preventDefault();
												handleAddIgnoreMessage();
											}
										}}
									/>
									<button type="button" className="btn btn-primary btn-xs" onClick={handleAddIgnoreMessage}>Add</button>
								</div>
							</div>
						</div>
					</section>

					{/* Instances Section */}
					<div className="grid grid-cols-1 gap-10">
						{/* Radarr */}
						<section className="space-y-4">
							<div className="mb-2 flex items-center justify-between">
								<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Radarr Instances</h4>
								<button type="button" className="btn btn-xs btn-primary btn-outline px-4" onClick={() => { setNewInstance({ ...DEFAULT_NEW_INSTANCE, type: "radarr" }); setShowAddInstance(true); }} disabled={isReadOnly}><Plus className="h-3 w-3" /> Add Radarr</button>
							</div>
							<div className="grid grid-cols-1 gap-4">
								{formData.radarr_instances.map((instance, index) => renderInstance(instance, "radarr", index))}
								{formData.radarr_instances.length === 0 && <div className="rounded-xl border border-base-300 border-dashed bg-base-200/30 py-6 text-center text-base-content/40 text-xs">No Radarr instances configured.</div>}
							</div>
						</section>

						{/* Sonarr */}
						<section className="space-y-4">
							<div className="mb-2 flex items-center justify-between">
								<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Sonarr Instances</h4>
								<button type="button" className="btn btn-xs btn-primary btn-outline px-4" onClick={() => { setNewInstance({ ...DEFAULT_NEW_INSTANCE, type: "sonarr", category: "tv" }); setShowAddInstance(true); }} disabled={isReadOnly}><Plus className="h-3 w-3" /> Add Sonarr</button>
							</div>
							<div className="grid grid-cols-1 gap-4">
								{formData.sonarr_instances.map((instance, index) => renderInstance(instance, "sonarr", index))}
								{formData.sonarr_instances.length === 0 && <div className="rounded-xl border border-base-300 border-dashed bg-base-200/30 py-6 text-center text-base-content/40 text-xs">No Sonarr instances configured.</div>}
							</div>
						</section>
					</div>

					{/* Save Button */}
					{!isReadOnly && (
						<div className="flex justify-end border-base-200 border-t pt-6">
							<button
								type="button"
								className={`btn btn-primary btn-md px-10 ${hasChanges ? "shadow-lg shadow-primary/20" : ""}`}
								onClick={handleSave}
								disabled={isUpdating || validationErrors.length > 0}
							>
								{isUpdating ? <span className="loading loading-spinner loading-sm" /> : <Save className="h-4 w-4" />}
								Save Arrs Configuration
							</button>
						</div>
					)}
				</div>
			)}

			{/* Add Instance Modal */}
			{showAddInstance && (
				<div className="modal modal-open">
					<div className="modal-box max-w-sm border border-base-300 p-4 shadow-2xl sm:max-w-md sm:p-6">
						<h3 className="mb-6 border-base-300 border-b pb-2 font-bold text-lg">Add {newInstance.type === "radarr" ? "Radarr" : "Sonarr"} Instance</h3>
						<div className="space-y-4">
							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-bold text-[10px]">NAME</legend>
								<input type="text" className="input input-bordered w-full" value={newInstance.name} onChange={(e) => setNewInstance({ ...newInstance, name: e.target.value })} placeholder="My Instance" />
							</fieldset>
							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-bold text-[10px]">URL</legend>
								<input type="url" className="input input-bordered w-full" value={newInstance.url} onChange={(e) => setNewInstance({ ...newInstance, url: e.target.value })} placeholder="http://192.168.1.10:7878" />
							</fieldset>
							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-bold text-[10px]">API KEY</legend>
								<input type="password" placeholder="••••••••" className="input input-bordered w-full" value={newInstance.api_key} onChange={(e) => setNewInstance({ ...newInstance, api_key: e.target.value })} />
							</fieldset>
							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-bold text-[10px]">CATEGORY (OPTIONAL)</legend>
								<select className="select select-bordered w-full" value={newInstance.category} onChange={(e) => setNewInstance({ ...newInstance, category: e.target.value })}>
									<option value="">Default</option>
									{config.sabnzbd?.categories?.map((cat) => <option key={cat.name} value={cat.name}>{cat.name}</option>)}
								</select>
							</fieldset>
						</div>
						<div className="modal-action mt-8">
							<button type="button" className="btn btn-ghost btn-sm" onClick={() => setShowAddInstance(false)}>Cancel</button>
							<button type="button" className="btn btn-primary btn-sm px-6" onClick={addInstance} disabled={!newInstance.name || !newInstance.url || !newInstance.api_key}>Add Instance</button>
						</div>
					</div>
				</div>
			)}

			{/* Validation Summary */}
			{validationErrors.length > 0 && (
				<div className="alert alert-warning border-warning/20 py-3 text-xs shadow-sm">
					<AlertTriangle className="h-4 w-4" />
					<ul className="ml-4 flex-1 list-disc space-y-1">
						{validationErrors.slice(0, 3).map((err, i) => <li key={i}>{err}</li>)}
						{validationErrors.length > 3 && <li>...and {validationErrors.length - 3} more issues</li>}
					</ul>
				</div>
			)}
		</div>
	);
}
