import { AlertTriangle, Plus, Save, Webhook } from "lucide-react";
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
// ... (omitted code) ...
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
// ... (omitted code) ...
		// Reset form and hide
		setNewInstance(DEFAULT_NEW_INSTANCE);
		setShowAddInstance(false);
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
						{webhookSuccess && <div className="alert alert-success mt-4 py-2">{webhookSuccess}</div>}
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
								<input
									type="text"
									className="input"
									value={newInstance.category}
									onChange={(e) => setNewInstance((prev) => ({ ...prev, category: e.target.value }))}
									placeholder={newInstance.type === "radarr" ? "movies" : "tv"}
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
