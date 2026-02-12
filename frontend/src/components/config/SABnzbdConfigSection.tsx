import { AlertTriangle, CheckCircle, Download, Plus, Save, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useRegisterArrsDownloadClients, useTestArrsDownloadClients } from "../../hooks/useApi";
import type { ConfigResponse, SABnzbdCategory, SABnzbdConfig } from "../../types/config";

interface SABnzbdConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: SABnzbdConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

interface NewCategoryForm {
	name: string;
	order: number;
	priority: number;
	dir: string;
}

const DEFAULT_NEW_CATEGORY: NewCategoryForm = {
	name: "",
	order: 1,
	priority: 0,
	dir: "",
};

const DEFAULT_CATEGORY_NAME = "Default";
const isDefaultCategory = (categoryName: string) => categoryName === DEFAULT_CATEGORY_NAME;

export function SABnzbdConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: SABnzbdConfigSectionProps) {
	const [formData, setFormData] = useState<SABnzbdConfig>(config.sabnzbd);
	const [hasChanges, setHasChanges] = useState(false);
	const [showAddCategory, setShowAddCategory] = useState(false);
	const [newCategory, setNewCategory] = useState<NewCategoryForm>(DEFAULT_NEW_CATEGORY);
	const [validationErrors, setValidationErrors] = useState<string[]>([]);
	const [fallbackApiKey, setFallbackApiKey] = useState<string>("");
	const [testResults, setTestResults] = useState<Record<string, string> | null>(null);

	const registerDownloadClient = useRegisterArrsDownloadClients();
	const testDownloadClient = useTestArrsDownloadClients();

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.sabnzbd);
		setHasChanges(false);
		setValidationErrors([]);
		setFallbackApiKey(""); // Reset API key field on config reload
		setTestResults(null);
	}, [config.sabnzbd]);

	const handleRegisterDownloadClient = async () => {
		try {
			await registerDownloadClient.mutateAsync();
		} catch (error) {
			console.error("Failed to register download client:", error);
		}
	};

	const handleTestDownloadClient = async () => {
		try {
			const results = await testDownloadClient.mutateAsync();
			setTestResults(results);
		} catch (error) {
			console.error("Failed to test connections:", error);
		}
	};

	const validateForm = (data: SABnzbdConfig): string[] => {
		const errors: string[] = [];

		if (data.enabled) {
			// Validate complete_dir is required and starts with /
			if (!data.complete_dir?.trim()) {
				errors.push("Complete directory is required when SABnzbd API is enabled");
			} else if (!data.complete_dir.startsWith("/")) {
				errors.push("Complete directory must start with /");
			}

			// Validate category names are unique
			const categoryNames = data.categories.map((cat) => cat.name);
			const duplicates = categoryNames.filter(
				(name, index) => categoryNames.indexOf(name) !== index,
			);
			if (duplicates.length > 0) {
				errors.push(`Duplicate category names found: ${[...new Set(duplicates)].join(", ")}`);
			}

			// Validate category names are not empty
			const emptyNames = data.categories.filter((cat) => !cat.name.trim());
			if (emptyNames.length > 0) {
				errors.push("Category names cannot be empty");
			}
		}

		return errors;
	};

	const updateFormData = (updates: Partial<SABnzbdConfig>) => {
		const newData = { ...formData, ...updates };
		const errors = validateForm(newData);
		setValidationErrors(errors);
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.sabnzbd));
	};

	const handleEnabledChange = (enabled: boolean) => {
		updateFormData({ enabled });
	};

	const handleCompleteDirChange = (complete_dir: string) => {
		updateFormData({ complete_dir });
	};

	const handleFallbackHostChange = (fallback_host: string) => {
		updateFormData({ fallback_host });
	};

	const handleFallbackApiKeyChange = (value: string) => {
		setFallbackApiKey(value);
		setHasChanges(true);
	};

	const handleCategoryUpdate = (index: number, updates: Partial<SABnzbdCategory>) => {
		const category = formData.categories[index];
		// For Default category, only prevent renaming (allow order, priority, dir changes)
		if (isDefaultCategory(category.name) && updates.name !== undefined) {
			// Prevent renaming Default category
			delete updates.name;
			if (Object.keys(updates).length === 0) {
				return;
			}
		}

		const categories = [...formData.categories];
		categories[index] = { ...categories[index], ...updates };
		updateFormData({ categories });
	};

	const handleRemoveCategory = (index: number) => {
		const category = formData.categories[index];
		// Prevent removing Default category
		if (isDefaultCategory(category.name)) {
			return;
		}
		const categories = formData.categories.filter((_, i) => i !== index);
		updateFormData({ categories });
	};

	const handleAddCategory = () => {
		if (!newCategory.name.trim()) {
			return;
		}

		// Prevent creating a category with the reserved name
		if (newCategory.name.trim() === DEFAULT_CATEGORY_NAME) {
			setValidationErrors([`"${DEFAULT_CATEGORY_NAME}" is a reserved category name`]);
			return;
		}

		const category: SABnzbdCategory = {
			name: newCategory.name.trim(),
			order: newCategory.order,
			priority: newCategory.priority,
			dir: newCategory.dir.trim(),
		};

		const categories = [...formData.categories, category].sort((a, b) => a.order - b.order);
		updateFormData({ categories });

		// Reset form
		setNewCategory(DEFAULT_NEW_CATEGORY);
		setShowAddCategory(false);
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges && validationErrors.length === 0) {
			// Remove fallback_api_key from formData to prevent sending placeholder
			const { fallback_api_key: _, ...configWithoutApiKey } = formData;
			const updateData: SABnzbdConfig & { fallback_api_key?: string } = configWithoutApiKey;
			// Only include API key if user entered a new value (not empty and not obfuscated placeholder)
			if (fallbackApiKey && fallbackApiKey !== "********") {
				updateData.fallback_api_key = fallbackApiKey;
			}
			await onUpdate("sabnzbd", updateData);
			setHasChanges(false);
			setFallbackApiKey(""); // Clear the password field after save
		}
	};

	const canSave = hasChanges && validationErrors.length === 0 && !isUpdating;

	return (
		<div className="space-y-10">
			{/* SABnzbd Service Status */}
			<section className="space-y-4">
				<div className="mb-2 flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Service Status</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>
				
				<div className="card border border-base-300 bg-base-200/50 shadow-sm">
					<div className="card-body p-4 sm:p-6">
						<div className="flex items-center justify-between gap-4">
							<div className="min-w-0 flex-1">
								<h3 className="flex items-center gap-2 font-bold text-sm sm:text-base">
									Enable SABnzbd API compatibility
									<span className="badge badge-ghost badge-xs font-mono uppercase">/sabnzbd</span>
								</h3>
								<p className="mt-1 text-base-content/60 text-xs leading-relaxed">
									Expose a compatible endpoint for download clients like Radarr/Sonarr.
								</p>
							</div>
							<input
								type="checkbox"
								className="toggle toggle-primary"
								checked={formData.enabled}
								onChange={(e) => handleEnabledChange(e.target.checked)}
								disabled={isReadOnly}
							/>
						</div>
					</div>
				</div>
			</section>

			{formData.enabled && (
				<div className="fade-in animate-in space-y-10 duration-500">
					{/* Virtual Filesystem Settings */}
					<section className="space-y-6">
						<div className="flex items-center gap-2">
							<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Virtual Filesystem</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">Complete Directory</legend>
								<input type="text" className="input w-full bg-base-200/50 font-mono" value={formData.complete_dir} disabled={isReadOnly} placeholder="/" onChange={(e) => handleCompleteDirChange(e.target.value)} />
								<p className="label text-[10px] opacity-60">Base path relative to mount point (usually /).</p>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">External API URL</legend>
								<input type="url" className="input w-full bg-base-200/50 font-mono" value={formData.download_client_base_url || ""} disabled={isReadOnly} placeholder="http://altmount:8080/sabnzbd" onChange={(e) => updateFormData({ download_client_base_url: e.target.value })} />
								<p className="label text-[10px] opacity-60">The URL ARR instances will use to communicate.</p>
							</fieldset>
						</div>
					</section>

					{/* Fallback Section */}
					<section className="space-y-6">
						<div className="flex items-center gap-2">
							<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">External Fallback (Optional)</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="grid grid-cols-1 gap-6 rounded-2xl border border-base-300 bg-base-200/30 p-6 lg:grid-cols-2">
							<div className="space-y-4">
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend font-semibold">Fallback Host</legend>
									<input type="text" className="input input-sm w-full bg-base-100 font-mono" value={formData.fallback_host || ""} disabled={isReadOnly} placeholder="http://localhost:8080" onChange={(e) => handleFallbackHostChange(e.target.value)} />
								</fieldset>
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend font-semibold">API Key {config.sabnzbd.fallback_api_key_set && <span className="badge badge-success badge-xs ml-1 origin-left scale-75 uppercase">Set</span>}</legend>
									<input type="password" className="input input-sm w-full bg-base-100 font-mono" value={fallbackApiKey} disabled={isReadOnly} placeholder="••••••••••••••••" onChange={(e) => handleFallbackApiKeyChange(e.target.value)} />
								</fieldset>
							</div>
							<div className="flex flex-col justify-center">
								<div className={`alert ${formData.fallback_host ? "alert-info" : "bg-base-100/50"} py-3 text-xs leading-relaxed`}>
									<AlertTriangle className="h-4 w-4 shrink-0" />
									<p>Failed imports can be automatically forwarded to a real SABnzbd instance for repair or alternate handling.</p>
								</div>
							</div>
						</div>
					</section>

					{/* Categories Section */}
					<section className="space-y-6">
						<div className="flex items-center justify-between">
							<div className="flex flex-1 items-center gap-2">
								<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Download Categories</h4>
								<div className="h-px flex-1 bg-base-300" />
							</div>
							<button type="button" className="btn btn-xs btn-primary btn-outline ml-4 px-4" onClick={() => setShowAddCategory(true)} disabled={isReadOnly}><Plus className="h-3 w-3" /> Add Category</button>
						</div>

						{showAddCategory && (
							<div className="card fade-in animate-in border-2 border-primary/30 bg-base-100 shadow-md duration-300">
								<div className="card-body p-4">
									<h5 className="mb-4 font-bold text-primary text-xs uppercase">Add New Category</h5>
									<div className="grid grid-cols-1 items-end gap-4 sm:grid-cols-2 lg:grid-cols-4">
										<fieldset className="fieldset min-w-0">
											<legend className="fieldset-legend font-bold text-[10px] opacity-50">NAME</legend>
											<input type="text" className="input input-xs w-full bg-base-200" value={newCategory.name} onChange={(e) => setNewCategory({ ...newCategory, name: e.target.value })} placeholder="e.g. movies-4k" />
										</fieldset>
										<fieldset className="fieldset min-w-0">
											<legend className="fieldset-legend font-bold text-[10px] opacity-50">SUBDIRECTORY</legend>
											<input type="text" className="input input-xs w-full bg-base-200 font-mono" value={newCategory.dir} onChange={(e) => setNewCategory({ ...newCategory, dir: e.target.value })} placeholder="Optional" />
										</fieldset>
										<div className="grid grid-cols-2 gap-4">
											<fieldset className="fieldset min-w-0">
												<legend className="fieldset-legend font-bold text-[10px] opacity-50">ORDER</legend>
												<input type="number" className="input input-xs w-full bg-base-200" value={newCategory.order} onChange={(e) => setNewCategory({ ...newCategory, order: Number.parseInt(e.target.value, 10) || 0 })} />
											</fieldset>
											<fieldset className="fieldset min-w-0">
												<legend className="fieldset-legend font-bold text-[10px] opacity-50">PRIORITY</legend>
												<select className="select select-xs w-full bg-base-200" value={newCategory.priority} onChange={(e) => setNewCategory({ ...newCategory, priority: Number.parseInt(e.target.value, 10) })}>
													<option value={-1}>Low</option>
													<option value={0}>Normal</option>
													<option value={1}>High</option>
												</select>
											</fieldset>
										</div>
										<div className="flex justify-end gap-2">
											<button type="button" className="btn btn-ghost btn-xs" onClick={() => setShowAddCategory(false)}>Cancel</button>
											<button type="button" className="btn btn-primary btn-xs px-4" onClick={handleAddCategory}>Add Category</button>
										</div>
									</div>
								</div>
							</div>
						)}

						<div className="grid grid-cols-1 gap-4">
							{formData.categories.sort((a, b) => a.order - b.order).map((category, index) => {
								const isDefault = isDefaultCategory(category.name);
								return (
									<div key={index} className={`card ${isDefault ? "border-primary/20 bg-primary/5" : "border-base-300 bg-base-200/50"} border shadow-sm`}>
										<div className="card-body p-4">
											<div className="grid grid-cols-1 items-end gap-4 sm:grid-cols-2 lg:grid-cols-4">
												<fieldset className="fieldset min-w-0">
													<legend className="fieldset-legend font-bold text-[10px] opacity-50">NAME</legend>
													<input type="text" className="input input-xs w-full bg-base-100 font-bold" value={category.name} disabled={isReadOnly || isDefault} onChange={(e) => handleCategoryUpdate(index, { name: e.target.value })} />
												</fieldset>
												<fieldset className="fieldset min-w-0">
													<legend className="fieldset-legend font-bold text-[10px] opacity-50">SUBDIRECTORY</legend>
													<input type="text" className="input input-xs w-full bg-base-100 font-mono" value={category.dir} disabled={isReadOnly} placeholder="Optional" onChange={(e) => handleCategoryUpdate(index, { dir: e.target.value })} />
												</fieldset>
												<div className="grid grid-cols-2 gap-4">
													<fieldset className="fieldset min-w-0">
														<legend className="fieldset-legend font-bold text-[10px] opacity-50">ORDER</legend>
														<input type="number" className="input input-xs w-full bg-base-100 font-mono" value={category.order} disabled={isReadOnly} onChange={(e) => handleCategoryUpdate(index, { order: Number.parseInt(e.target.value, 10) || 0 })} />
													</fieldset>
													<fieldset className="fieldset min-w-0">
														<legend className="fieldset-legend font-bold text-[10px] opacity-50">PRIORITY</legend>
														<select className="select select-xs w-full bg-base-100" value={category.priority} disabled={isReadOnly} onChange={(e) => handleCategoryUpdate(index, { priority: Number.parseInt(e.target.value, 10) })}>
															<option value={-1}>Low</option>
															<option value={0}>Normal</option>
															<option value={1}>High</option>
														</select>
													</fieldset>
												</div>
												<div className="flex justify-end">
													{!isDefault && !isReadOnly && (
														<button type="button" className="btn btn-ghost btn-xs text-error" onClick={() => handleRemoveCategory(index)}><Trash2 className="h-3.5 w-3.5" /></button>
													)}
													{isDefault && <span className="badge badge-primary badge-outline badge-xs px-2 font-bold uppercase">System</span>}
												</div>
											</div>
										</div>
									</div>
								);
							})}
						</div>
					</section>

					{/* Action Buttons */}
					{!isReadOnly && (
						<div className="flex flex-col gap-6 border-base-200 border-t pt-6">
							<div className="flex flex-wrap justify-end gap-3">
								<button type="button" className="btn btn-outline btn-sm px-6" onClick={handleTestDownloadClient} disabled={testDownloadClient.isPending}><CheckCircle className="h-4 w-4" /> Test Connectivity</button>
								<button type="button" className="btn btn-outline btn-info btn-sm px-6" onClick={handleRegisterDownloadClient} disabled={registerDownloadClient.isPending}><Download className="h-4 w-4" /> Auto-Setup ARR</button>
								<button type="button" className={`btn btn-primary btn-md px-10 ${hasChanges ? "shadow-lg shadow-primary/20" : ""}`} onClick={handleSave} disabled={!canSave}><Save className="h-4 w-4" /> Save SABnzbd Configuration</button>
							</div>

							{testResults && (
								<div className="fade-in animate-in rounded-xl border border-base-300 bg-base-200/50 p-4 duration-300">
									<span className="mb-3 block font-bold text-[10px] uppercase opacity-50">Connection Test Results</span>
									<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-3">
										{Object.entries(testResults).map(([instance, result]) => (
											<div key={instance} className="flex items-center justify-between rounded-lg border border-base-200 bg-base-100 p-2 px-3">
												<span className="mr-2 truncate font-semibold text-xs">{instance}</span>
												<span className={`rounded-full px-2 py-0.5 font-mono text-[10px] ${result === "OK" ? "bg-success/10 text-success" : "bg-error/10 text-error"}`}>{result}</span>
											</div>
										))}
									</div>
								</div>
							)}
						</div>
					)}
				</div>
			)}

			{!formData.enabled && (
				<div className="alert border-info/20 bg-info/5 py-6 shadow-sm">
					<AlertTriangle className="h-6 w-6 text-info" />
					<div>
						<h4 className="font-bold text-info">SABnzbd API Interface Disabled</h4>
						<p className="mt-1 text-sm opacity-80">Enable this to allow Radarr/Sonarr to communicate with AltMount as if it were a local SABnzbd instance.</p>
					</div>
				</div>
			)}
		</div>
	);
}
