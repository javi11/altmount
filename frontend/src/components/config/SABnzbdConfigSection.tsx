import {
	AlertTriangle,
	CheckCircle,
	Download,
	Eye,
	EyeOff,
	Info,
	Plus,
	Save,
	Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useRegisterArrsDownloadClients, useTestArrsDownloadClients } from "../../hooks/useApi";
import type { ConfigResponse, SABnzbdCategory, SABnzbdConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

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
	const [showApiKey, setShowApiKey] = useState(false);
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
			if (!data.complete_dir?.trim()) {
				errors.push("Complete directory is required when SABnzbd API is enabled");
			} else if (!data.complete_dir.startsWith("/")) {
				errors.push("Complete directory must start with /");
			}
			const categoryNames = data.categories.map((cat) => cat.name);
			const duplicates = categoryNames.filter(
				(name, index) => categoryNames.indexOf(name) !== index,
			);
			if (duplicates.length > 0)
				errors.push(`Duplicate category names: ${[...new Set(duplicates)].join(", ")}`);
			if (data.categories.some((cat) => !cat.name.trim()))
				errors.push("Category names cannot be empty");
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

	const handleCategoryUpdate = (index: number, updates: Partial<SABnzbdCategory>) => {
		const category = formData.categories[index];
		if (isDefaultCategory(category.name) && updates.name !== undefined) {
			delete updates.name;
			if (Object.keys(updates).length === 0) return;
		}
		const categories = [...formData.categories];
		categories[index] = { ...categories[index], ...updates };
		updateFormData({ categories });
	};

	const handleRemoveCategory = (index: number) => {
		if (isDefaultCategory(formData.categories[index].name)) return;
		const categories = formData.categories.filter((_, i) => i !== index);
		updateFormData({ categories });
	};

	const handleAddCategory = () => {
		if (!newCategory.name.trim()) return;
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
		setNewCategory(DEFAULT_NEW_CATEGORY);
		setShowAddCategory(false);
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges && validationErrors.length === 0) {
			// Remove fallback_api_key from formData to prevent sending placeholder
			const { fallback_api_key: _, ...configWithoutApiKey } = formData;
			const updateData: SABnzbdConfig & { fallback_api_key?: string } = configWithoutApiKey;
			if (fallbackApiKey && fallbackApiKey !== "********") {
				updateData.fallback_api_key = fallbackApiKey;
			}
			await onUpdate("sabnzbd", updateData);
			setHasChanges(false);
			setFallbackApiKey("");
		}
	};

	return (
		<div className="space-y-10">
			<div>
				<h3 className="font-bold text-base-content text-lg tracking-tight">SABnzbd Endpoint</h3>
				<p className="break-words text-base-content/50 text-sm">
					Emulate a SABnzbd server to allow ARR applications to send NZBs to AltMount.
				</p>
			</div>

			<div className="space-y-8">
				{/* Enable Toggle */}
				<div className="rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-start justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h4 className="break-words font-bold text-base-content text-sm">
								Virtual API Server
							</h4>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Provides standard SABnzbd endpoints at <code>/sabnzbd</code> for full compatibility.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary mt-1 shrink-0"
							checked={formData.enabled}
							disabled={isReadOnly}
							onChange={(e) => updateFormData({ enabled: e.target.checked })}
						/>
					</div>
				</div>

				{formData.enabled && (
					<>
						{/* Basic Paths */}
						<div className="fade-in slide-in-from-top-2 animate-in space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
							<div className="flex items-center gap-2">
								<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
									Base Config
								</h4>
								<div className="h-px flex-1 bg-base-300/50" />
							</div>

							<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
								<fieldset className="fieldset">
									<legend className="fieldset-legend font-semibold">
										Virtual Root (Complete Dir)
									</legend>
									<input
										type="text"
										className="input input-bordered w-full bg-base-100 font-mono text-sm"
										value={formData.complete_dir}
										readOnly={isReadOnly}
										placeholder="/"
										onChange={(e) => updateFormData({ complete_dir: e.target.value })}
									/>
									<p className="label break-words text-base-content/70 text-xs">
										Relative to your mount point.
									</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend font-semibold">Public Callback URL</legend>
									<input
										type="url"
										className="input input-bordered w-full bg-base-100 font-mono text-sm"
										value={formData.download_client_base_url || ""}
										onChange={(e) => updateFormData({ download_client_base_url: e.target.value })}
										placeholder="http://altmount:8080/sabnzbd"
										disabled={isReadOnly}
									/>
									<p className="label break-words text-base-content/70 text-xs">
										The URL ARR instances use to reach this API.
									</p>
								</fieldset>
							</div>
						</div>

						{/* External Fallback */}
						<div className="fade-in slide-in-from-top-4 animate-in space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
							<div className="flex items-center gap-2">
								<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
									Failover Engine
								</h4>
								<div className="h-px flex-1 bg-base-300/50" />
							</div>

							<div className="space-y-6">
								<div className="rounded-xl border border-info/10 bg-info/5 p-4">
									<div className="flex gap-3">
										<Info className="mt-0.5 h-4 w-4 shrink-0 text-info" />
										<p className="break-words text-[11px] leading-relaxed opacity-80">
											Failover allows internal processing failures to be automatically sent to a
											real external SABnzbd instance after max retries.
										</p>
									</div>
								</div>

								<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
									<fieldset className="fieldset">
										<legend className="fieldset-legend font-semibold">External Host</legend>
										<input
											type="text"
											className="input input-bordered w-full bg-base-100 font-mono text-sm"
											value={formData.fallback_host || ""}
											readOnly={isReadOnly}
											placeholder="http://192.168.1.10:8080"
											onChange={(e) => updateFormData({ fallback_host: e.target.value })}
										/>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend font-semibold">API Key</legend>
										<div className="relative">
											<input
												type={showApiKey ? "text" : "password"}
												className="input input-bordered w-full bg-base-100 pr-10 font-mono text-sm"
												value={fallbackApiKey}
												readOnly={isReadOnly}
												placeholder={
													formData.fallback_host && config.sabnzbd.fallback_api_key_set
														? "••••••••••••••••"
														: "Paste API key..."
												}
												onChange={(e) => {
													setFallbackApiKey(e.target.value);
													setHasChanges(true);
												}}
											/>
											<button
												type="button"
												className="-translate-y-1/2 btn btn-ghost btn-sm absolute top-1/2 right-2"
												onClick={() => setShowApiKey(!showApiKey)}
											>
												{showApiKey ? (
													<EyeOff className="h-4 w-4 text-base-content/70" />
												) : (
													<Eye className="h-4 w-4 text-base-content/70" />
												)}
											</button>
										</div>
									</fieldset>
								</div>
							</div>
						</div>

						{/* Categories */}
						<div className="fade-in slide-in-from-top-6 animate-in space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
							<div className="flex items-center justify-between gap-4">
								<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
									Category Mapping
								</h4>
								{!isReadOnly && (
									<button
										type="button"
										className="btn btn-sm btn-primary px-4 shadow-sm"
										onClick={() => setShowAddCategory(true)}
									>
										<Plus className="h-3 w-3" /> Add
									</button>
								)}
							</div>

							<div className="space-y-3">
								{formData.categories
									.sort((a, b) => a.order - b.order)
									.map((cat, idx) => {
										const isDefault = isDefaultCategory(cat.name);
										return (
											<div
												key={idx}
												className={`group relative rounded-xl border p-4 transition-all ${isDefault ? "border-primary/20 bg-primary/5" : "border-base-300 bg-base-100/50 hover:bg-base-100"}`}
											>
												{isDefault && (
													<span className="absolute top-2 right-3 font-black text-[8px] text-base-content/80 text-primary uppercase tracking-widest">
														System Core
													</span>
												)}

												<div className="grid grid-cols-1 gap-4 sm:grid-cols-4">
													<fieldset className="fieldset">
														<legend className="fieldset-legend font-black text-base-content/60 text-xs uppercase">
															Label
														</legend>
														<input
															type="text"
															className="input input-sm input-bordered bg-base-100 font-bold"
															value={cat.name}
															readOnly={isReadOnly || isDefault}
															onChange={(e) => handleCategoryUpdate(idx, { name: e.target.value })}
														/>
													</fieldset>
													<fieldset className="fieldset">
														<legend className="fieldset-legend font-black text-base-content/60 text-xs uppercase">
															Order
														</legend>
														<input
															type="number"
															className="input input-sm input-bordered bg-base-100 font-mono"
															value={cat.order}
															readOnly={isReadOnly}
															onChange={(e) =>
																handleCategoryUpdate(idx, {
																	order: Number.parseInt(e.target.value, 10) || 0,
																})
															}
														/>
													</fieldset>
													<fieldset className="fieldset">
														<legend className="fieldset-legend font-black text-base-content/60 text-xs uppercase">
															Priority
														</legend>
														<select
															className="select select-sm select-bordered bg-base-100"
															value={cat.priority}
															disabled={isReadOnly}
															onChange={(e) =>
																handleCategoryUpdate(idx, {
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
														<legend className="fieldset-legend font-black text-base-content/60 text-xs uppercase">
															Dir Mapping
														</legend>
														<div className="flex items-center gap-2">
															<input
																type="text"
																className="input input-sm input-bordered flex-1 bg-base-100 font-mono text-[11px]"
																value={cat.dir}
																readOnly={isReadOnly}
																placeholder={isDefault ? "complete" : "optional"}
																onChange={(e) => handleCategoryUpdate(idx, { dir: e.target.value })}
															/>
															{!isReadOnly && !isDefault && (
																<button
																	type="button"
																	className="btn btn-square btn-ghost btn-sm text-error opacity-0 transition-opacity group-hover:opacity-100"
																	onClick={() => handleRemoveCategory(idx)}
																>
																	<Trash2 className="h-3.5 w-3.5" />
																</button>
															)}
														</div>
													</fieldset>
												</div>
											</div>
										);
									})}
							</div>

							{showAddCategory && (
								<div className="zoom-in-95 animate-in space-y-5 rounded-xl border-2 border-primary/30 border-dashed bg-primary/5 p-5">
									<h5 className="font-bold text-xs">New Category Definition</h5>
									<div className="grid grid-cols-1 gap-4 sm:grid-cols-4">
										<input
											type="text"
											className="input input-sm input-bordered"
											placeholder="Label (e.g. movies)"
											value={newCategory.name}
											onChange={(e) => setNewCategory({ ...newCategory, name: e.target.value })}
										/>
										<input
											type="number"
											className="input input-sm input-bordered"
											placeholder="Order"
											value={newCategory.order}
											onChange={(e) =>
												setNewCategory({
													...newCategory,
													order: Number.parseInt(e.target.value, 10) || 1,
												})
											}
										/>
										<select
											className="select select-sm select-bordered"
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
										<input
											type="text"
											className="input input-sm input-bordered"
											placeholder="Subdir (optional)"
											value={newCategory.dir}
											onChange={(e) => setNewCategory({ ...newCategory, dir: e.target.value })}
										/>
									</div>
									<div className="flex justify-end gap-2">
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
											className="btn btn-primary btn-sm px-4"
											onClick={handleAddCategory}
											disabled={!newCategory.name.trim()}
										>
											Save Definition
										</button>
									</div>
								</div>
							)}
						</div>
					</>
				)}
			</div>

			{/* Actions & Save */}
			<div className="space-y-6 border-base-200 border-t pt-6">
				{validationErrors.map((err, idx) => (
					<div key={idx} className="alert alert-error rounded-xl py-2 text-xs shadow-sm">
						<AlertTriangle className="h-4 w-4" /> <span className="break-words">{err}</span>
					</div>
				))}

				{!isReadOnly && (
					<div className="flex flex-col gap-4">
						<div className="flex flex-wrap justify-end gap-3">
							<button
								type="button"
								className="btn btn-ghost btn-sm border-base-300 bg-base-100 hover:bg-base-200"
								onClick={handleTestDownloadClient}
								disabled={testDownloadClient.isPending}
							>
								{testDownloadClient.isPending ? (
									<LoadingSpinner size="sm" />
								) : (
									<CheckCircle className="h-4 w-4" />
								)}
								Test ARR Links
							</button>
							<button
								type="button"
								className="btn btn-ghost btn-sm border-base-300 bg-base-100 hover:bg-base-200"
								onClick={handleRegisterDownloadClient}
								disabled={registerDownloadClient.isPending || hasChanges}
							>
								{registerDownloadClient.isPending ? (
									<LoadingSpinner size="sm" />
								) : (
									<Download className="h-4 w-4" />
								)}
								Auto-Setup Clients
							</button>
							<button
								type="button"
								className="btn btn-primary btn-sm px-10 shadow-lg shadow-primary/20"
								onClick={handleSave}
								disabled={!hasChanges || validationErrors.length > 0 || isUpdating}
							>
								{isUpdating ? <LoadingSpinner size="sm" /> : <Save className="h-4 w-4" />}
								{isUpdating ? "Saving..." : "Save Changes"}
							</button>
						</div>

						{regSuccess && (
							<div className="alert alert-success rounded-xl py-2 text-xs">{regSuccess}</div>
						)}
						{regError && (
							<div className="alert alert-error rounded-xl py-2 text-xs">{regError}</div>
						)}

						{testResults && (
							<div className="rounded-xl border border-base-300 bg-base-200/50 p-4">
								<div className="mb-3 font-black text-base-content/60 text-xs uppercase tracking-widest">
									Connectivity Health
								</div>
								<div className="space-y-2">
									{Object.entries(testResults).map(([instance, result]) => (
										<div key={instance} className="flex items-center justify-between text-xs">
											<span className="font-medium opacity-70">{instance}</span>
											<span
												className={`font-bold font-mono ${result === "OK" ? "text-success" : "text-error"}`}
											>
												{result}
											</span>
										</div>
									))}
								</div>
							</div>
						)}
					</div>
				)}
			</div>
		</div>
	);
}
