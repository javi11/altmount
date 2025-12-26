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
	const [regSuccess, setRegSuccess] = useState<string | null>(null);
	const [regError, setRegError] = useState<string | null>(null);
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
		setRegSuccess(null);
		setRegError(null);
		setTestResults(null);
		try {
			await registerDownloadClient.mutateAsync();
			setRegSuccess("Download client registration triggered successfully.");
			// Hide success message after 5 seconds
			setTimeout(() => setRegSuccess(null), 5000);
		} catch (error) {
			setRegError(error instanceof Error ? error.message : "Failed to register download client.");
		}
	};

	const handleTestDownloadClient = async () => {
		setRegSuccess(null);
		setRegError(null);
		setTestResults(null);
		try {
			const results = await testDownloadClient.mutateAsync();
			setTestResults(results);
		} catch (error) {
			setRegError(error instanceof Error ? error.message : "Failed to test connections.");
		}
	};

	const validateForm = (data: SABnzbdConfig): string[] => {
		const errors: string[] = [];

		if (data.enabled) {
			// Validate complete_dir is required and absolute
			if (!data.complete_dir?.trim()) {
				errors.push("Complete directory is required when SABnzbd API is enabled");
			} else if (!data.complete_dir.startsWith("/")) {
				errors.push("Complete directory must be an absolute path (starting with /)");
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
		const categories = [...formData.categories];
		categories[index] = { ...categories[index], ...updates };
		updateFormData({ categories });
	};

	const handleRemoveCategory = (index: number) => {
		const categories = formData.categories.filter((_, i) => i !== index);
		updateFormData({ categories });
	};

	const handleAddCategory = () => {
		if (!newCategory.name.trim()) {
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
		<div className="space-y-6">
			<h3 className="font-semibold text-lg">SABnzbd API Configuration</h3>

			{/* Validation Errors */}
			{validationErrors.length > 0 && (
				<div className="alert alert-error">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">Configuration Errors</div>
						<ul className="mt-2 list-inside list-disc text-sm">
							{validationErrors.map((error, index) => (
								<li key={index}>{error}</li>
							))}
						</ul>
					</div>
				</div>
			)}

			{/* Enable/Disable Toggle */}
			<fieldset className="fieldset">
				<legend className="fieldset-legend">Enable SABnzbd API</legend>
				<label className="label cursor-pointer">
					<span className="label-text">
						Enable SABnzbd-compatible API endpoint for download clients
					</span>
					<input
						type="checkbox"
						className="toggle toggle-primary"
						checked={formData.enabled}
						disabled={isReadOnly}
						onChange={(e) => handleEnabledChange(e.target.checked)}
					/>
				</label>
				<p className="label">
					When enabled, provides SABnzbd-compatible API endpoints at <code>/sabnzbd</code>
				</p>
			</fieldset>

			{/* Configuration when enabled */}
			{formData.enabled && (
				<>
					{/* Complete Directory */}
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Complete directory</legend>
						<input
							type="text"
							className="input"
							value={formData.complete_dir}
							readOnly={isReadOnly}
							placeholder="/mnt/altmount/complete"
							onChange={(e) => handleCompleteDirChange(e.target.value)}
						/>
						<p className="label">
							Absolute path to the directory where the full imports will be stored, relative to the
							mounted folder.
						</p>
					</fieldset>

					{/* Download Client Base URL */}
					<fieldset className="fieldset">
						<legend className="fieldset-legend">AltMount URL (for download clients)</legend>
						<input
							type="url"
							className="input"
							value={formData.download_client_base_url || ""}
							onChange={(e) => updateFormData({ download_client_base_url: e.target.value })}
							placeholder="http://altmount:8080/sabnzbd"
							disabled={isReadOnly}
						/>
						<p className="label">
							The URL ARR instances will use to talk back to AltMount SABnzbd API. Default: <code>http://altmount:8080/sabnzbd</code>
						</p>
					</fieldset>

					{/* Fallback SABnzbd Configuration */}
					<div className="space-y-4">
						<div>
							<h4 className="font-medium">Fallback to External SABnzbd (Optional)</h4>
							<p className="text-base-content/70 text-sm">
								Configure an external SABnzbd instance to automatically receive failed imports after
								max retries. This provides a fallback when internal processing fails.
							</p>
						</div>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">Fallback SABnzbd Host</legend>
							<input
								type="text"
								name="fallback_host"
								autoComplete="off"
								className="input"
								value={formData.fallback_host || ""}
								readOnly={isReadOnly}
								placeholder="http://localhost:8080 or https://sabnzbd.example.com"
								onChange={(e) => handleFallbackHostChange(e.target.value)}
							/>
							<p className="label">
								URL of the external SABnzbd instance (including http:// or https://)
							</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">Fallback SABnzbd API Key</legend>
							<input
								type="password"
								name="fallback_api_key"
								autoComplete="new-password"
								className="input"
								value={fallbackApiKey}
								readOnly={isReadOnly}
								placeholder={
									formData.fallback_host && config.sabnzbd.fallback_api_key_set
										? "••••••••••••••••"
										: "Enter API key"
								}
								onChange={(e) => handleFallbackApiKeyChange(e.target.value)}
							/>
							<p className="label">
								API key for the external SABnzbd instance. Leave empty to keep existing key.
								{config.sabnzbd.fallback_api_key_set && " (Currently set)"}
							</p>
						</fieldset>

						{formData.fallback_host && (
							<div className="alert alert-info">
								<div>
									<div className="font-bold">Fallback Enabled</div>
									<div className="text-sm">
										Failed imports will be automatically sent to{" "}
										<code>{formData.fallback_host}</code> after max retries are exceeded.
									</div>
								</div>
							</div>
						)}
					</div>

					{/* Categories Section */}
					<div className="space-y-4">
						<div className="flex items-center justify-between">
							<div>
								<h4 className="font-medium">Categories</h4>
								<p className="text-base-content/70 text-sm">
									Configure download categories for organization. If no categories are configured, a
									default category will be used.
								</p>
							</div>
							{!isReadOnly && (
								<button
									type="button"
									className="btn btn-outline btn-sm"
									onClick={() => setShowAddCategory(true)}
								>
									<Plus className="h-4 w-4" />
									Add Category
								</button>
							)}
						</div>

						{/* Existing Categories */}
						{formData.categories.length > 0 && (
							<div className="space-y-3">
								{formData.categories
									.sort((a, b) => a.order - b.order)
									.map((category, index) => (
										<div key={index} className="card bg-base-200 shadow-sm">
											<div className="card-body p-4">
												<div className="grid grid-cols-1 gap-4 md:grid-cols-4">
													<fieldset className="fieldset">
														<legend className="fieldset-legend">Name</legend>
														<input
															type="text"
															className="input input-sm"
															value={category.name}
															readOnly={isReadOnly}
															onChange={(e) =>
																handleCategoryUpdate(index, { name: e.target.value })
															}
														/>
													</fieldset>
													<fieldset className="fieldset">
														<legend className="fieldset-legend">Order</legend>
														<input
															type="number"
															className="input input-sm"
															value={category.order}
															readOnly={isReadOnly}
															onChange={(e) =>
																handleCategoryUpdate(index, {
																	order: Number.parseInt(e.target.value, 10) || 0,
																})
															}
														/>
													</fieldset>
													<fieldset className="fieldset">
														<legend className="fieldset-legend">Priority</legend>
														<select
															className="select select-sm"
															value={category.priority}
															disabled={isReadOnly}
															onChange={(e) =>
																handleCategoryUpdate(index, {
																	priority: Number.parseInt(e.target.value, 10),
																})
															}
														>
															<option value={-1}>Low</option>
															<option value={0}>Normal</option>
															<option value={1}>High</option>
														</select>
													</fieldset>
													<fieldset className="fieldset">
														<legend className="fieldset-legend">Subdirectory</legend>
														<input
															type="text"
															className="input input-sm"
															value={category.dir}
															readOnly={isReadOnly}
															placeholder="Optional subdirectory"
															onChange={(e) => handleCategoryUpdate(index, { dir: e.target.value })}
														/>
													</fieldset>
												</div>
												{!isReadOnly && (
													<div className="mt-2 flex justify-end">
														<button
															type="button"
															className="btn btn-ghost btn-sm btn-error"
															onClick={() => handleRemoveCategory(index)}
														>
															<Trash2 className="h-4 w-4" />
															Remove
														</button>
													</div>
												)}
											</div>
										</div>
									))}
							</div>
						)}

						{/* Add Category Form */}
						{showAddCategory && !isReadOnly && (
							<div className="card border-2 border-primary border-dashed bg-base-200 shadow-sm">
								<div className="card-body p-4">
									<h5 className="mb-3 font-medium">Add New Category</h5>
									<div className="grid grid-cols-1 gap-4 md:grid-cols-4">
										<fieldset className="fieldset">
											<legend className="fieldset-legend">Name</legend>
											<input
												type="text"
												className="input input-sm"
												value={newCategory.name}
												placeholder="movies"
												onChange={(e) => setNewCategory({ ...newCategory, name: e.target.value })}
											/>
										</fieldset>
										<fieldset className="fieldset">
											<legend className="fieldset-legend">Order</legend>
											<input
												type="number"
												className="input input-sm"
												value={newCategory.order}
												onChange={(e) =>
													setNewCategory({
														...newCategory,
														order: Number.parseInt(e.target.value, 10) || 1,
													})
												}
											/>
										</fieldset>
										<fieldset className="fieldset">
											<legend className="fieldset-legend">Priority</legend>
											<select
												className="select select-sm"
												value={newCategory.priority}
												onChange={(e) =>
													setNewCategory({
														...newCategory,
														priority: Number.parseInt(e.target.value, 10),
													})
												}
											>
												<option value={-1}>Low</option>
												<option value={0}>Normal</option>
												<option value={1}>High</option>
											</select>
										</fieldset>
										<fieldset className="fieldset">
											<legend className="fieldset-legend">Subdirectory</legend>
											<input
												type="text"
												className="input input-sm"
												value={newCategory.dir}
												placeholder="movies"
												onChange={(e) => setNewCategory({ ...newCategory, dir: e.target.value })}
											/>
										</fieldset>
									</div>
									<div className="mt-3 flex justify-end space-x-2">
										<button
											type="button"
											className="btn btn-ghost btn-sm"
											onClick={() => {
												setShowAddCategory(false);
												setNewCategory(DEFAULT_NEW_CATEGORY);
											}}
										>
											Cancel
										</button>
										<button
											type="button"
											className="btn btn-primary btn-sm"
											onClick={handleAddCategory}
											disabled={!newCategory.name.trim()}
										>
											Add Category
										</button>
									</div>
								</div>
							</div>
						)}

						{/* Empty State */}
						{formData.categories.length === 0 && !showAddCategory && (
							<div className="py-8 text-center text-base-content/70">
								<p>No categories configured. A default category will be used.</p>
								{!isReadOnly && (
									<button
										type="button"
										className="btn btn-outline btn-sm mt-2"
										onClick={() => setShowAddCategory(true)}
									>
										<Plus className="h-4 w-4" />
										Add First Category
									</button>
								)}
							</div>
						)}
					</div>
				</>
			)}

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex flex-col space-y-4">
					<div className="flex justify-end space-x-2">
						<button
							type="button"
							className="btn btn-outline btn-secondary"
							onClick={handleTestDownloadClient}
							disabled={testDownloadClient.isPending}
							title="Test if enabled ARR instances can connect back to AltMount"
						>
							{testDownloadClient.isPending ? (
								<span className="loading loading-spinner loading-sm" />
							) : (
								<CheckCircle className="h-4 w-4" />
							)}
							{testDownloadClient.isPending ? "Testing..." : "Test ARR Connectivity"}
						</button>
						<button
							type="button"
							className="btn btn-outline btn-info"
							onClick={handleRegisterDownloadClient}
							disabled={registerDownloadClient.isPending}
							title="Register AltMount as a SABnzbd download client in all enabled ARR instances"
						>
							{registerDownloadClient.isPending ? (
								<span className="loading loading-spinner loading-sm" />
							) : (
								<Download className="h-4 w-4" />
							)}
							{registerDownloadClient.isPending ? "Registering..." : "Auto-Setup ARR Download Clients"}
						</button>
						<button
							type="button"
							className="btn btn-primary"
							onClick={handleSave}
							disabled={!canSave}
						>
							{isUpdating ? (
								<span className="loading loading-spinner loading-sm" />
							) : (
								<Save className="h-4 w-4" />
							)}
							{isUpdating ? "Saving..." : "Save Changes"}
						</button>
					</div>
					{regSuccess && <div className="alert alert-success py-2">{regSuccess}</div>}
					{regError && <div className="alert alert-error py-2">{regError}</div>}
					{testResults && (
						<div className="alert bg-base-200 border-base-300 py-2">
							<div className="flex flex-col w-full">
								<div className="font-bold mb-1">Connection Test Results:</div>
								<div className="space-y-1">
									{Object.entries(testResults).map(([instance, result]) => (
										<div key={instance} className="flex justify-between text-sm">
											<span>{instance}:</span>
											<span className={result === "OK" ? "text-success" : "text-error font-mono"}>
												{result}
											</span>
										</div>
									))}
								</div>
							</div>
						</div>
					)}
				</div>
			)}

			{/* Information when disabled */}
			{!formData.enabled && (
				<div className="alert alert-info">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">SABnzbd API Disabled</div>
						<div className="text-sm">
							Enable the SABnzbd API to make AltMount compatible with SABnzbd download clients.
							You'll need to configure the complete directory and optionally set up categories.
						</div>
					</div>
				</div>
			)}
		</div>
	);
}
