import { AlertTriangle, Plus, Save, Trash2, Webhook } from "lucide-react";
import { useEffect, useState } from "react";
import { useRegisterArrsWebhooks } from "../../hooks/useApi";
import type { ArrsConfig, ArrsInstanceConfig, ArrsType, ConfigResponse } from "../../types/config";
import ArrsInstanceCard from "./ArrsInstanceCard";
import { LoadingSpinner } from "../ui/LoadingSpinner";

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
			if (!config.mount_path) {
				errors.push("Mount Path must be configured in General/System settings before enabling Arrs service");
			}
			const allInstanceNames = [
				...data.radarr_instances.map((i) => ({ name: i.name, type: "Radarr" })),
				...data.sonarr_instances.map((i) => ({ name: i.name, type: "Sonarr" })),
			];
			const nameCount: Record<string, number> = {};
			allInstanceNames.forEach(({ name }) => {
				nameCount[name] = (nameCount[name] || 0) + 1;
			});
			Object.entries(nameCount).forEach(([name, count]) => {
				if (count > 1) errors.push(`Instance name "${name}" is used multiple times`);
			});
			[...data.radarr_instances, ...data.sonarr_instances].forEach((instance, index) => {
				const instanceType = data.radarr_instances.includes(instance) ? "Radarr" : "Sonarr";
				if (!instance.name.trim()) errors.push(`${instanceType} instance #${index + 1}: Name is required`);
				if (!instance.url.trim()) {
					errors.push(`${instanceType} instance "${instance.name}": URL is required`);
				} else {
					try { new URL(instance.url); } catch { errors.push(`${instanceType} instance "${instance.name}": Invalid URL format`); }
				}
				if (!instance.api_key.trim()) errors.push(`${instanceType} instance "${instance.name}": API key is required`);
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
		if (!newInstance.name.trim() || !newInstance.url.trim() || !newInstance.api_key.trim()) return;
		const instancesKey = newInstance.type === "radarr" ? "radarr_instances" : "sonarr_instances";
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
		setShowApiKeys((prev) => ({ ...prev, [instanceId]: !prev[instanceId] }));
	};

	const renderInstance = (instance: ArrsInstanceConfig, type: ArrsType, index: number) => {
		const instanceId = `${type}-${index}`;
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
			<div>
				<h3 className="text-lg font-bold text-base-content tracking-tight">ARR Applications</h3>
				<p className="text-sm text-base-content/50 break-words">Connect Radarr and Sonarr for automatic health monitoring and repair.</p>
			</div>

			<div className="space-y-8">
				{/* Enable/Disable Arrs */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6">
					<div className="flex items-start justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h4 className="font-bold text-sm text-base-content break-words">Service Engine</h4>
							<p className="text-[11px] text-base-content/50 mt-1 break-words leading-relaxed">
								Allows AltMount to talk to Radarr/Sonarr for repairs and updates.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary shrink-0 mt-1"
							checked={formData.enabled}
							onChange={(e) => handleFormChange("enabled", e.target.checked)}
							disabled={isReadOnly}
						/>
					</div>
				</div>

				{/* Webhooks Auto-Registration */}
				{formData.enabled && (
					<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6 animate-in fade-in slide-in-from-top-2">
						<div className="flex items-center gap-2">
							<h4 className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">Automation</h4>
							<div className="h-px flex-1 bg-base-300/50" />
						</div>

						<div className="space-y-6">
							<div>
								<h5 className="font-bold text-sm">AltMount Webhooks</h5>
								<p className="text-[11px] text-base-content/50 mt-1 break-words leading-relaxed">
									Automatically configure hooks in Radarr/Sonarr to notify AltMount of upgrades and renames.
								</p>
							</div>

							<div className="flex flex-col gap-4 sm:flex-row sm:items-end">
								<fieldset className="fieldset flex-1">
									<legend className="fieldset-legend font-semibold">AltMount Callback URL</legend>
									<input
										type="url"
										className="input input-bordered w-full bg-base-100 text-sm font-mono"
										value={formData.webhook_base_url ?? "http://altmount:8080"}
										onChange={(e) => handleFormChange("webhook_base_url", e.target.value)}
										placeholder="http://altmount:8080"
										disabled={isReadOnly}
									/>
								</fieldset>

								<button
									type="button"
									className="btn btn-primary btn-sm px-6 shadow-lg shadow-primary/20 shrink-0"
									onClick={handleRegisterWebhooks}
									disabled={isReadOnly || registerWebhooks.isPending || hasChanges}
								>
									{registerWebhooks.isPending ? <LoadingSpinner size="sm" /> : <Webhook className="h-4 w-4" />}
									{registerWebhooks.isPending ? "Connecting..." : "Setup Webhooks"}
								</button>
							</div>
							
							{hasChanges && <p className="text-[10px] text-warning font-bold">Save changes before configuring webhooks.</p>}
							{webhookSuccess && <div className="alert alert-success py-2 text-xs rounded-xl">{webhookSuccess}</div>}
							{webhookError && <div className="alert alert-error py-2 text-xs rounded-xl">{webhookError}</div>}
						</div>
					</div>
				)}

				{/* Queue Cleanup Settings */}
				{formData.enabled && (
					<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6 animate-in fade-in slide-in-from-top-4">
						<div className="flex items-center gap-2">
							<h4 className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">Maintenance</h4>
							<div className="h-px flex-1 bg-base-300/50" />
						</div>

						<div className="flex items-start justify-between gap-4">
							<div className="min-w-0 flex-1">
								<h5 className="font-bold text-sm text-base-content break-words">Queue Auto-Cleanup</h5>
								<p className="text-[11px] text-base-content/50 mt-1 break-words leading-relaxed">
									Automatically remove empty import folders from ARR queues.
								</p>
							</div>
							<input
								type="checkbox"
								className="checkbox checkbox-primary checkbox-sm shrink-0 mt-1"
								checked={formData.queue_cleanup_enabled ?? true}
								onChange={(e) => handleFormChange("queue_cleanup_enabled", e.target.checked)}
								disabled={isReadOnly}
							/>
						</div>

						{(formData.queue_cleanup_enabled ?? true) && (
							<div className="space-y-6 animate-in fade-in zoom-in-95">
								<fieldset className="fieldset max-w-xs">
									<legend className="fieldset-legend font-semibold">Cleanup Interval</legend>
									<div className="join w-full">
										<input
											type="number"
											className="input input-bordered join-item w-full bg-base-100 font-mono text-sm"
											value={formData.queue_cleanup_interval_seconds ?? 10}
											onChange={(e) => handleFormChange("queue_cleanup_interval_seconds", parseInt(e.target.value) || 10)}
											min={1}
											max={3600}
											disabled={isReadOnly}
										/>
										<span className="btn btn-ghost border-base-300 join-item pointer-events-none text-xs">sec</span>
									</div>
								</fieldset>

								<div className="grid grid-cols-1 sm:grid-cols-2 gap-6">
									<fieldset className="fieldset">
										<legend className="fieldset-legend font-semibold">Cleanup Grace Period</legend>
										<div className="join w-full">
											<input
												type="number"
												className="input input-bordered join-item w-full bg-base-100 font-mono text-sm"
												value={formData.queue_cleanup_grace_period_minutes ?? 10}
												onChange={(e) => handleFormChange("queue_cleanup_grace_period_minutes", parseInt(e.target.value) || 10)}
												min={0}
												disabled={isReadOnly}
											/>
											<span className="btn btn-ghost border-base-300 join-item pointer-events-none text-xs">min</span>
										</div>
										<p className="label text-[10px] opacity-50 break-words mt-1">Wait time before considering a failed item "stuck" and eligible for cleanup.</p>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend font-semibold">Import Failure Cleanup</legend>
										<label className="label cursor-pointer justify-start gap-4 items-center h-12">
											<input
												type="checkbox"
												className="toggle toggle-primary toggle-sm"
												checked={formData.cleanup_automatic_import_failure ?? false}
												onChange={(e) => handleFormChange("cleanup_automatic_import_failure", e.target.checked)}
												disabled={isReadOnly}
											/>
											<span className="label-text font-bold text-xs break-words">Purge Automatic Failures</span>
										</label>
										<p className="label text-[10px] opacity-50 break-words mt-1">Automatically remove items from queue that failed with "Automatic Import" errors.</p>
									</fieldset>
								</div>

								<div className="space-y-4">
									<h5 className="font-bold text-[10px] uppercase opacity-40">Allowlist (Ignore Errors)</h5>
									<div className="space-y-2 max-h-48 overflow-y-auto pr-2 custom-scrollbar">
										{(formData.queue_cleanup_allowlist || []).map((msg, index) => (
											<div key={index} className="flex items-center justify-between rounded-xl bg-base-100/50 border border-base-300/50 p-2 pl-3">
												<div className="flex items-center gap-3 min-w-0 flex-1">
													<input
														type="checkbox"
														className="checkbox checkbox-xs checkbox-primary"
														checked={msg.enabled}
														onChange={() => handleToggleIgnoreMessage(index)}
														disabled={isReadOnly}
													/>
													<span className={`truncate font-mono text-[10px] ${!msg.enabled ? "opacity-30 line-through" : ""}`} title={msg.message}>
														{msg.message}
													</span>
												</div>
												<button type="button" className="btn btn-ghost btn-xs text-error hover:bg-error/10" onClick={() => handleRemoveIgnoreMessage(index)} disabled={isReadOnly}>
													<Trash2 className="h-3 w-3" />
												</button>
											</div>
										))}
									</div>

									{!isReadOnly && (
										<div className="join w-full shadow-sm">
											<input
												type="text"
												className="input input-bordered join-item flex-1 bg-base-100 text-xs"
												placeholder="Add error message to ignore..."
												value={newIgnoreMessage}
												onChange={(e) => setNewIgnoreMessage(e.target.value)}
												onKeyDown={(e) => e.key === "Enter" && handleAddIgnoreMessage()}
											/>
											<button type="button" className="btn btn-primary join-item px-4" onClick={handleAddIgnoreMessage} disabled={!newIgnoreMessage.trim()}>
												<Plus className="h-4 w-4" />
											</button>
										</div>
									)}
								</div>
							</div>
						)}
					</div>
				)}

				{/* Instances Lists */}
				{formData.enabled && (
					<div className="space-y-10 animate-in fade-in slide-in-from-top-6">
						{/* Radarr */}
						<div className="space-y-6">
							<div className="flex items-center justify-between gap-4">
								<h4 className="font-bold text-sm flex items-center gap-2">
									<div className="h-2 w-2 rounded-full bg-primary" /> Radarr Instances
								</h4>
								<button
									type="button"
									className="btn btn-xs btn-primary px-4"
									onClick={() => { setNewInstance({ ...DEFAULT_NEW_INSTANCE, type: "radarr" }); setShowAddInstance(true); }}
									disabled={isReadOnly}
								>
									<Plus className="h-3 w-3" /> Add
								</button>
							</div>
							<div className="grid grid-cols-1 gap-4">
								{formData.radarr_instances.map((instance, index) => renderInstance(instance, "radarr", index))}
								{formData.radarr_instances.length === 0 && (
									<div className="rounded-2xl border-2 border-dashed border-base-300 p-8 text-center opacity-40 text-xs font-bold">No Radarr configured</div>
								)}
							</div>
						</div>

						{/* Sonarr */}
						<div className="space-y-6">
							<div className="flex items-center justify-between gap-4">
								<h4 className="font-bold text-sm flex items-center gap-2">
									<div className="h-2 w-2 rounded-full bg-secondary" /> Sonarr Instances
								</h4>
								<button
									type="button"
									className="btn btn-xs btn-primary px-4"
									onClick={() => { setNewInstance({ ...DEFAULT_NEW_INSTANCE, type: "sonarr", category: "tv" }); setShowAddInstance(true); }}
									disabled={isReadOnly}
								>
									<Plus className="h-3 w-3" /> Add
								</button>
							</div>
							<div className="grid grid-cols-1 gap-4">
								{formData.sonarr_instances.map((instance, index) => renderInstance(instance, "sonarr", index))}
								{formData.sonarr_instances.length === 0 && (
									<div className="rounded-2xl border-2 border-dashed border-base-300 p-8 text-center opacity-40 text-xs font-bold">No Sonarr configured</div>
								)}
							</div>
						</div>
					</div>
				)}
			</div>

			{/* Modal for adding instance */}
			{showAddInstance && (
				<div className="modal modal-open backdrop-blur-sm">
					<div className="modal-box rounded-2xl border border-base-300 shadow-2xl">
						<h3 className="font-black text-xl uppercase tracking-tighter mb-6">
							Add {newInstance.type === "radarr" ? "Radarr" : "Sonarr"}
						</h3>
						<div className="space-y-5">
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-bold">Friendly Name</legend>
								<input type="text" className="input input-bordered w-full" value={newInstance.name} onChange={(e) => setNewInstance(prev => ({...prev, name: e.target.value}))} placeholder="My ARR Server" />
							</fieldset>
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-bold">Server URL</legend>
								<input type="url" className="input input-bordered w-full font-mono text-sm" value={newInstance.url} onChange={(e) => setNewInstance(prev => ({...prev, url: e.target.value}))} placeholder="http://192.168.1.10:7878" />
							</fieldset>
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-bold">API Key</legend>
								<input type="password" title="API Key" className="input input-bordered w-full font-mono text-sm" value={newInstance.api_key} onChange={(e) => setNewInstance(prev => ({...prev, api_key: e.target.value}))} />
							</fieldset>
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-bold">Category Mapping</legend>
								<select className="select select-bordered w-full" value={newInstance.category} onChange={(e) => setNewInstance(prev => ({...prev, category: e.target.value}))}>
									<option value="">(Auto Detect)</option>
									{config.sabnzbd?.categories?.map(cat => <option key={cat.name} value={cat.name}>{cat.name}</option>)}
								</select>
							</fieldset>
						</div>
						<div className="modal-action gap-3">
							<button type="button" className="btn btn-ghost" onClick={() => { setShowAddInstance(false); setNewInstance(DEFAULT_NEW_INSTANCE); }}>Cancel</button>
							<button type="button" className="btn btn-primary px-8 shadow-lg shadow-primary/20" onClick={addInstance} disabled={!newInstance.name.trim() || !newInstance.url.trim() || !newInstance.api_key.trim()}>Add Server</button>
						</div>
					</div>
				</div>
			)}

			{/* Validation & Save */}
			<div className="space-y-4 pt-6 border-t border-base-200">
				{validationErrors.map((error, idx) => (
					<div key={idx} className="alert alert-warning py-2 text-xs rounded-xl border border-warning/20 bg-warning/5">
						<AlertTriangle className="h-4 w-4 shrink-0" />
						<span className="break-words">{error}</span>
					</div>
				))}
				{saveError && <div className="alert alert-error py-2 text-xs rounded-xl">{saveError}</div>}
				
				{hasChanges && (
					<div className="flex justify-end">
						<button type="button" className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${isUpdating ? "loading" : ""}`} onClick={handleSave} disabled={isUpdating || validationErrors.length > 0}>
							{!isUpdating && <Save className="h-4 w-4" />}
							{isUpdating ? "Saving..." : "Save Changes"}
						</button>
					</div>
				)}
			</div>
		</div>
	);
}
