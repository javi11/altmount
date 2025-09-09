import { AlertTriangle, Plus, Save, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
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

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.sabnzbd);
		setHasChanges(false);
		setValidationErrors([]);
	}, [config.sabnzbd]);

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
			await onUpdate("sabnzbd", formData);
			setHasChanges(false);
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
					When enabled, provides SABnzbd-compatible API endpoints at <code>/sabnzbd/api</code>
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
							Absolute path to the directory where the complete imports will be placed. FROM THE
							MOUNTED FOLDER POINT OF VIEW.
						</p>
					</fieldset>

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
				<div className="flex justify-end">
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
