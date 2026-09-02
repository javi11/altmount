import {
	Globe,
	Loader2,
	Plus,
	RefreshCw,
	Server,
	ShieldCheck,
	Trash2,
	XCircle,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import type {
	NewsnabIndexerConfig,
	ProwlarrConfig,
	ProwlarrIndexer,
	StremioConfig,
	StremioIndexersConfig,
	UserAgentInfo,
} from "../../../types/config";

interface IndexersConfigCardProps {
	config: StremioConfig;
	onChange: (updated: StremioConfig) => void;
	isReadOnly?: boolean;
}

// Category presets for common indexer profiles. The list is shared by movie
// and TV searches for the indexer, so mixed lists only suit indexers that
// tolerate them (NZBgeek does; altHUB does not).
const CATEGORY_PRESETS: { label: string; value: string; title: string }[] = [
	{
		label: "TV + Anime",
		value: "5000, 5010, 5030, 5040, 5070",
		title:
			"For anime-focused indexers — several file anime under 5070, outside the TV defaults. On general indexers (e.g. NZBgeek) this restricts movie searches on this indexer.",
	},
	{
		label: "Movies",
		value: "2000, 2010, 2030, 2040, 2045, 2060",
		title: "For movie-focused indexers. Restricts TV searches on this indexer.",
	},
	{
		label: "Movies + TV + Anime",
		value: "2000, 2010, 2030, 2040, 2045, 2060, 5000, 5010, 5030, 5040, 5070",
		title:
			"The universal choice when in doubt — verified against altHUB and NZBgeek for both movie and TV searches.",
	},
];

const CATEGORIES_HELP_TEXT =
	"Space- or comma-separated Newznab category IDs. Leave empty for automatic per-type defaults (recommended). A custom list applies to both movie and TV searches on this indexer.";

// parseCategoriesInput converts the comma/space-separated category text into a
// deduplicated numeric list. It returns null when any token is not a valid
// non-negative integer so callers can surface a validation error.
const parseCategoriesInput = (raw: string): number[] | null => {
	const trimmed = raw.trim();
	if (!trimmed) return [];
	const cats: number[] = [];
	for (const token of trimmed.split(/[,\s]+/).filter(Boolean)) {
		if (!/^\d+$/.test(token)) return null;
		const value = Number(token);
		if (!Number.isInteger(value) || value > 999999) return null;
		if (!cats.includes(value)) cats.push(value);
	}
	return cats;
};

export function IndexersConfigCard({
	config,
	onChange,
	isReadOnly = false,
}: IndexersConfigCardProps) {
	const indexersConfig: StremioIndexersConfig = config.indexers || {
		provider: "prowlarr",
		user_agent_mode: "auto",
		custom_user_agent: "",
		prowlarr: config.prowlarr,
		newsnab: [],
	};

	const prowlarr = indexersConfig.prowlarr || config.prowlarr;
	const newsnabList = indexersConfig.newsnab || [];

	// Local State for Indexer fetching
	const [prowlarrIndexers, setProwlarrIndexers] = useState<ProwlarrIndexer[]>([]);
	const [isFetchingIndexers, setIsFetchingIndexers] = useState(false);
	const [fetchError, setFetchError] = useState<string | null>(null);

	// User Agent state
	const [userAgentInfo, setUserAgentInfo] = useState<UserAgentInfo | null>(null);
	const [isRefreshingUA, setIsRefreshingUA] = useState(false);

	// Newsnab Modal state
	const [isNewsnabModalOpen, setIsNewsnabModalOpen] = useState(false);
	const [editingNewsnab, setEditingNewsnab] = useState<NewsnabIndexerConfig | null>(null);
	const [newsnabForm, setNewsnabForm] = useState<Partial<NewsnabIndexerConfig>>({
		name: "",
		url: "",
		api_key: "",
		weight: 0,
		timeout_seconds: 4,
		enabled: true,
	});
	const [newsnabTestStatus, setNewsnabTestStatus] = useState<{
		loading: boolean;
		success?: boolean;
		message?: string;
	}>({ loading: false });
	const [categoriesText, setCategoriesText] = useState("");
	const [categoriesError, setCategoriesError] = useState<string | null>(null);

	// Fetch live user agent metadata
	useEffect(() => {
		fetch("/api/stremio/useragents")
			.then((res) => (res.ok ? res.json() : null))
			.then((data) => {
				if (data?.data) {
					setUserAgentInfo(data.data);
				}
			})
			.catch(() => {});
	}, []);

	const handleRefreshUA = async () => {
		setIsRefreshingUA(true);
		try {
			const res = await fetch("/api/stremio/useragents/refresh", { method: "POST" });
			if (res.ok) {
				const data = await res.json();
				if (data?.data) {
					setUserAgentInfo(data.data);
				}
			}
		} catch {
			// ignore
		} finally {
			setIsRefreshingUA(false);
		}
	};

	const updateIndexersConfig = (patch: Partial<StremioIndexersConfig>) => {
		const updated: StremioIndexersConfig = {
			...indexersConfig,
			...patch,
		};
		onChange({
			...config,
			indexers: updated,
			prowlarr: updated.prowlarr, // sync legacy
		});
	};

	const handleProviderChange = (provider: "prowlarr" | "newsnab" | "both") => {
		updateIndexersConfig({ provider });
	};

	const handleProwlarrFieldChange = <K extends keyof ProwlarrConfig>(
		field: K,
		value: ProwlarrConfig[K],
	) => {
		const updatedProwlarr = { ...prowlarr, [field]: value };
		updateIndexersConfig({ prowlarr: updatedProwlarr });
	};

	// Read through a ref so manual fetches send the key currently in the form
	// without making the fetch callback depend on it (which would refetch on
	// every keystroke via the auto-load effect).
	const prowlarrKeyRef = useRef(prowlarr.api_key);
	prowlarrKeyRef.current = prowlarr.api_key;

	const handleFetchProwlarrIndexers = useCallback(async () => {
		if (!prowlarr.host) {
			setFetchError("Prowlarr host is required to load indexers");
			return;
		}
		setIsFetchingIndexers(true);
		setFetchError(null);
		try {
			// An empty key means the stored credentials are masked; the backend
			// falls back to the saved Prowlarr configuration.
			const res = await fetch("/api/prowlarr/indexers", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					host: prowlarr.host,
					api_key: prowlarrKeyRef.current || "",
				}),
			});
			const data = await res.json();
			if (!res.ok) {
				throw new Error(data.message || data.error || "Failed to fetch indexers");
			}
			setProwlarrIndexers(data.data || []);
		} catch (e) {
			setFetchError(e instanceof Error ? e.message : "Connection failed");
		} finally {
			setIsFetchingIndexers(false);
		}
		// apiKeyRef keeps the form's current key fresh; the host is the only
		// reactive dependency so the auto-load effect does not refetch on
		// every keystroke.
	}, [prowlarr.host]);

	// Load the indexer list automatically so the selection checkboxes are
	// visible without a manual "Test & Load Indexers" click.
	useEffect(() => {
		if (!prowlarr.host || isReadOnly) return;
		handleFetchProwlarrIndexers();
	}, [prowlarr.host, handleFetchProwlarrIndexers, isReadOnly]);

	const toggleProwlarrIndexer = (id: number) => {
		const current = prowlarr.indexers ?? [];
		const next = current.includes(id) ? current.filter((i) => i !== id) : [...current, id];
		handleProwlarrFieldChange("indexers", next);
	};

	// Newsnab CRUD Handlers
	const openAddNewsnabModal = () => {
		setEditingNewsnab(null);
		setNewsnabForm({
			id: `newsnab_${Date.now()}`,
			name: "",
			url: "",
			api_key: "",
			weight: 0,
			timeout_seconds: 4,
			enabled: true,
		});
		setCategoriesText("");
		setCategoriesError(null);
		setNewsnabTestStatus({ loading: false });
		setIsNewsnabModalOpen(true);
	};

	const openEditNewsnabModal = (item: NewsnabIndexerConfig) => {
		setEditingNewsnab(item);
		setNewsnabForm({ ...item });
		setCategoriesText(item.categories?.length ? item.categories.join(", ") : "");
		setCategoriesError(null);
		setNewsnabTestStatus({ loading: false });
		setIsNewsnabModalOpen(true);
	};

	// Discovered indexer capabilities, surfaced by the Test Connection probe.
	interface DiscoveredCapabilities {
		search?: boolean;
		movie_search?: boolean;
		tv_search?: boolean;
		search_params?: string[];
		movie_params?: string[];
		tv_params?: string[];
		category_count?: number;
	}
	const [discoveredCaps, setDiscoveredCaps] = useState<DiscoveredCapabilities | null>(null);

	const handleTestNewsnab = async (url: string, apiKey: string, indexerId?: string) => {
		setNewsnabTestStatus({ loading: true });
		setDiscoveredCaps(null);
		try {
			const res = await fetch("/api/stremio/indexers/newsnab/test", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					id: indexerId,
					url,
					api_key: apiKey,
				}),
			});
			const data = await res.json();
			if (res.ok && data?.data?.status === "ok") {
				setDiscoveredCaps(data.data.capabilities ?? null);
				setNewsnabTestStatus({
					loading: false,
					success: true,
					message: `Connected successfully! (${data.data.server_name || "Newznab Indexer"})`,
				});
			} else {
				setNewsnabTestStatus({
					loading: false,
					success: false,
					message: data?.message || data?.error || "Connection test failed",
				});
			}
		} catch (err) {
			setNewsnabTestStatus({
				loading: false,
				success: false,
				message: err instanceof Error ? err.message : "Connection failed",
			});
		}
	};

	const handleSaveNewsnab = () => {
		// When editing, the stored API key is masked in API responses and an
		// empty value keeps it (the backend restores it when patching).
		if (!newsnabForm.name || !newsnabForm.url || (!editingNewsnab && !newsnabForm.api_key)) {
			return;
		}

		const categories = parseCategoriesInput(categoriesText);
		if (categories === null) {
			setCategoriesError(
				"Categories must be space- or comma-separated Newznab IDs (e.g. 5000, 5070)",
			);
			return;
		}

		const current = [...newsnabList];
		const item: NewsnabIndexerConfig = {
			id: editingNewsnab ? editingNewsnab.id : `newsnab_${Date.now()}`,
			name: newsnabForm.name.trim(),
			url: newsnabForm.url.trim(),
			api_key: (newsnabForm.api_key ?? "").trim(),
			categories,
			weight: Number(newsnabForm.weight) || 0,
			timeout_seconds: Number(newsnabForm.timeout_seconds) || 4,
			enabled: newsnabForm.enabled ?? true,
		};

		let updated: NewsnabIndexerConfig[];
		if (editingNewsnab) {
			updated = current.map((x) => (x.id === item.id ? item : x));
		} else {
			updated = [...current, item];
		}

		updateIndexersConfig({ newsnab: updated });
		setIsNewsnabModalOpen(false);
	};

	const handleDeleteNewsnab = (id: string) => {
		const updated = newsnabList.filter((x) => x.id !== id);
		updateIndexersConfig({ newsnab: updated });
	};

	const handleToggleNewsnab = (id: string, enabled: boolean) => {
		const updated = newsnabList.map((x) => (x.id === id ? { ...x, enabled } : x));
		updateIndexersConfig({ newsnab: updated });
	};

	return (
		<div className="card border border-base-300 bg-base-200/50 shadow-sm">
			<div className="card-body space-y-6 p-6">
				{/* Section Header */}
				<div className="flex flex-wrap items-center justify-between gap-3 border-base-300 border-b pb-4">
					<div className="flex items-center gap-3">
						<div className="rounded-xl bg-secondary/10 p-2.5 text-secondary">
							<Server className="h-5 w-5" />
						</div>
						<div>
							<h3 className="font-bold text-base text-base-content">
								Search Providers & Indexer Dispatch
							</h3>
							<p className="text-base-content/60 text-xs">
								Configure parallel Usenet indexer queries via Prowlarr or direct Newznab endpoints.
							</p>
						</div>
					</div>
				</div>

				{/* 1. Provider Mode Selection */}
				<div className="space-y-3">
					<span className="font-semibold text-base-content/80 text-xs uppercase tracking-wider">
						Search Provider Architecture
					</span>
					<div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
						{[
							{
								id: "prowlarr",
								title: "Prowlarr Only",
								desc: "Search via unified Prowlarr indexer proxy",
							},
							{
								id: "newsnab",
								title: "Direct Newsnab",
								desc: "Query Newznab indexers directly without proxy",
							},
							{
								id: "both",
								title: "Both (Parallel Fan-out)",
								desc: "Simultaneously search Prowlarr & Newsnab in parallel",
							},
						].map((opt) => {
							const isSelected = indexersConfig.provider === opt.id;
							return (
								<button
									key={opt.id}
									type="button"
									disabled={isReadOnly}
									onClick={() => handleProviderChange(opt.id as "prowlarr" | "newsnab" | "both")}
									className={`flex flex-col items-start rounded-xl border p-4 text-left transition-all ${
										isSelected
											? "border-primary bg-primary/10 shadow-sm"
											: "border-base-300 bg-base-100/60 hover:bg-base-100"
									}`}
								>
									<div className="flex w-full items-center justify-between">
										<span className="font-bold text-base-content text-sm">{opt.title}</span>
										<input
											type="radio"
											name="search_provider"
											checked={isSelected}
											readOnly
											className="radio radio-primary radio-xs"
										/>
									</div>
									<span className="mt-1 text-[11px] text-base-content/60">{opt.desc}</span>
								</button>
							);
						})}
					</div>
				</div>

				{/* 2. Outbound User-Agent Configuration & Auto-Updater */}
				<div className="space-y-3 rounded-xl border border-base-300 bg-base-100/70 p-4">
					<div className="flex flex-wrap items-center justify-between gap-2">
						<div className="flex items-center gap-2">
							<ShieldCheck className="h-4 w-4 text-primary" />
							<span className="font-semibold text-base-content text-xs">
								Indexer Client Identification (User-Agent)
							</span>
						</div>
						<button
							type="button"
							onClick={handleRefreshUA}
							disabled={isRefreshingUA}
							className="btn btn-ghost btn-xs gap-1.5 text-primary"
							title="Check for latest official Sonarr and Radarr versions"
						>
							<RefreshCw className={`h-3 w-3 ${isRefreshingUA ? "animate-spin" : ""}`} />
							<span>Check Latest Version</span>
						</button>
					</div>

					<div className="flex flex-wrap items-center gap-2">
						<div className="flex items-center gap-1.5 rounded-lg border border-base-300 bg-base-200/60 px-3 py-1.5 text-xs">
							<span className="text-base-content/50">📺 TV Queries:</span>
							<span className="font-mono font-semibold text-primary">
								{userAgentInfo?.tv_user_agent || "Sonarr/4.1.1.824 (alpine 3.23.3)"}
							</span>
						</div>
						<div className="flex items-center gap-1.5 rounded-lg border border-base-300 bg-base-200/60 px-3 py-1.5 text-xs">
							<span className="text-base-content/50">🎬 Movie Queries:</span>
							<span className="font-mono font-semibold text-secondary">
								{userAgentInfo?.movie_user_agent || "Radarr/6.5.1.2032 (alpine 3.23.3)"}
							</span>
						</div>
						{userAgentInfo?.source && (
							<span className="badge badge-ghost badge-xs text-base-content/60">
								Source: {userAgentInfo.source.replace("_", " ")}
							</span>
						)}
					</div>

					<p className="text-[11px] text-base-content/50">
						Altmount automatically mimics native Sonarr/Radarr client headers so indexers optimize
						release responses. You can also specify an explicit override below.
					</p>

					<div className="flex items-center gap-2 pt-1">
						<input
							type="text"
							placeholder="Optional custom User-Agent override (leave blank for automatic)"
							value={indexersConfig.custom_user_agent || ""}
							disabled={isReadOnly}
							onChange={(e) => updateIndexersConfig({ custom_user_agent: e.target.value })}
							className="input input-sm flex-1 font-mono text-xs"
						/>
					</div>
				</div>

				{/* 3. Direct Newsnab Section */}
				{(indexersConfig.provider === "newsnab" || indexersConfig.provider === "both") && (
					<div className="space-y-3 rounded-xl border border-base-300 bg-base-100/60 p-4">
						<div className="flex flex-wrap items-center justify-between gap-2 border-base-300 border-b pb-3">
							<div>
								<h4 className="font-bold text-base-content text-sm">Direct Newsnab Indexers</h4>
								<p className="text-base-content/60 text-xs">
									Add Usenet indexers directly using their Newznab API key and endpoint.
								</p>
							</div>
							<button
								type="button"
								onClick={openAddNewsnabModal}
								disabled={isReadOnly}
								className="btn btn-primary btn-xs gap-1.5 shadow-sm"
							>
								<Plus className="h-3.5 w-3.5" />
								<span>Add Indexer</span>
							</button>
						</div>

						{newsnabList.length === 0 ? (
							<div className="rounded-lg border border-base-300 border-dashed py-6 text-center text-base-content/50 text-xs">
								No direct Newsnab indexers configured yet. Click "+ Add Indexer" to add one.
							</div>
						) : (
							<div className="overflow-x-auto">
								<table className="table-sm table">
									<thead>
										<tr className="border-base-300 text-[11px] text-base-content/60 uppercase tracking-wider">
											<th className="w-12 text-center">Active</th>
											<th>Indexer Name</th>
											<th>Endpoint URL</th>
											<th className="w-24 text-center">Weight</th>
											<th className="w-24 text-center">Timeout</th>
											<th className="w-24 text-center">Categories</th>
											<th className="w-20 text-right">Actions</th>
										</tr>
									</thead>
									<tbody>
										{newsnabList.map((item) => (
											<tr key={item.id} className="hover:bg-base-200/40">
												<td className="text-center">
													<input
														type="checkbox"
														checked={item.enabled}
														disabled={isReadOnly}
														onChange={(e) => handleToggleNewsnab(item.id, e.target.checked)}
														className="checkbox checkbox-xs checkbox-primary"
													/>
												</td>
												<td className="font-semibold text-base-content text-xs">{item.name}</td>
												<td className="max-w-xs truncate font-mono text-base-content/70 text-xs">
													{item.url}
												</td>
												<td className="text-center font-mono text-xs">
													<span className="badge badge-ghost badge-xs">+{item.weight || 0}</span>
												</td>
												<td className="text-center font-mono text-base-content/60 text-xs">
													{item.timeout_seconds || 4}s
												</td>
												<td className="text-center">
													{item.categories?.length ? (
														<span
															className="badge badge-ghost badge-xs"
															title={item.categories.join(", ")}
														>
															{item.categories.length} cats
														</span>
													) : (
														<span className="text-base-content/40 text-xs">auto</span>
													)}
												</td>
												<td className="text-right">
													<div className="flex items-center justify-end gap-1">
														<button
															type="button"
															onClick={() => openEditNewsnabModal(item)}
															className="btn btn-ghost btn-xs"
														>
															Edit
														</button>
														<button
															type="button"
															onClick={() => handleDeleteNewsnab(item.id)}
															className="btn btn-ghost btn-xs text-error"
														>
															<Trash2 className="h-3 w-3" />
														</button>
													</div>
												</td>
											</tr>
										))}
									</tbody>
								</table>
							</div>
						)}
					</div>
				)}

				{/* 4. Prowlarr Section */}
				{(indexersConfig.provider === "prowlarr" || indexersConfig.provider === "both") && (
					<div className="space-y-4 rounded-xl border border-base-300 bg-base-100/60 p-4">
						<div className="flex flex-wrap items-center justify-between gap-2 border-base-300 border-b pb-3">
							<div>
								<h4 className="font-bold text-base-content text-sm">Prowlarr Proxy Integration</h4>
								<p className="text-base-content/60 text-xs">
									Connect to your Prowlarr instance to query all configured usenet indexers.
								</p>
							</div>
							<div className="flex items-center gap-2">
								{(prowlarr.indexers?.length ?? 0) > 0 && (
									<span className="badge badge-primary badge-xs">
										{prowlarr.indexers?.length} selected
									</span>
								)}
								<button
									type="button"
									onClick={handleFetchProwlarrIndexers}
									disabled={isFetchingIndexers || isReadOnly || !prowlarr.host}
									className="btn btn-outline btn-xs gap-1.5"
								>
									{isFetchingIndexers ? (
										<Loader2 className="h-3 w-3 animate-spin" />
									) : (
										<Globe className="h-3 w-3" />
									)}
									<span>Test & Load Indexers</span>
								</button>
							</div>
						</div>

						{fetchError && (
							<div className="alert alert-error alert-sm text-xs">
								<XCircle className="h-4 w-4 shrink-0" />
								<span>{fetchError}</span>
							</div>
						)}

						<div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold text-xs">Prowlarr Host URL</legend>
								<input
									type="text"
									placeholder="http://prowlarr:9696"
									value={prowlarr.host || ""}
									disabled={isReadOnly}
									onChange={(e) => handleProwlarrFieldChange("host", e.target.value)}
									className="input input-sm w-full font-mono text-xs"
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold text-xs">Prowlarr API Key</legend>
								<input
									type="password"
									placeholder={
										prowlarr.api_key_set ? "Stored — leave empty to keep" : "Prowlarr API Key"
									}
									value={prowlarr.api_key || ""}
									disabled={isReadOnly}
									onChange={(e) => handleProwlarrFieldChange("api_key", e.target.value)}
									className="input input-sm w-full font-mono text-xs"
								/>
							</fieldset>
						</div>

						{/* Prowlarr Indexer selection */}
						{prowlarrIndexers.length > 0 && (
							<div className="space-y-2 pt-2">
								<div className="flex items-center justify-between gap-2">
									<span className="font-semibold text-base-content/70 text-xs">
										Discovered Prowlarr Indexers ({prowlarrIndexers.length})
									</span>
									<span className="text-[11px] text-base-content/50">
										Leave all unchecked to search across every configured indexer.
									</span>
								</div>
								<div className="grid max-h-40 grid-cols-1 gap-1 overflow-y-auto rounded-lg border border-base-300 bg-base-200/40 p-2 sm:grid-cols-2">
									{prowlarrIndexers.map((idx) => {
										const selected = prowlarr.indexers?.includes(idx.id) ?? false;
										return (
											<label
												key={idx.id}
												className="flex cursor-pointer items-center gap-2 rounded px-2 py-1 hover:bg-base-200"
											>
												<input
													type="checkbox"
													className="checkbox checkbox-xs checkbox-primary"
													checked={selected}
													disabled={isReadOnly}
													onChange={() => toggleProwlarrIndexer(idx.id)}
												/>
												<span className="min-w-0 flex-1 truncate text-xs">{idx.name}</span>
												{!idx.enable && (
													<span className="badge badge-ghost badge-xs">disabled</span>
												)}
											</label>
										);
									})}
								</div>
							</div>
						)}
					</div>
				)}
			</div>

			{/* Newsnab Add/Edit Modal */}
			{isNewsnabModalOpen && (
				<dialog className="modal modal-open">
					<div className="modal-box max-w-md space-y-4">
						<h3 className="font-bold text-base text-base-content">
							{editingNewsnab ? "Edit Newsnab Indexer" : "Add Direct Newsnab Indexer"}
						</h3>

						<div className="space-y-3">
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold text-xs">Indexer Name</legend>
								<input
									type="text"
									placeholder="e.g. NZBGeek"
									value={newsnabForm.name || ""}
									onChange={(e) => setNewsnabForm({ ...newsnabForm, name: e.target.value })}
									className="input input-sm w-full text-xs"
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold text-xs">API Endpoint URL</legend>
								<input
									type="text"
									placeholder="https://api.nzbgeek.info"
									value={newsnabForm.url || ""}
									onChange={(e) => setNewsnabForm({ ...newsnabForm, url: e.target.value })}
									className="input input-sm w-full font-mono text-xs"
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold text-xs">API Key</legend>
								<input
									type="password"
									placeholder={
										editingNewsnab && editingNewsnab.api_key_set !== false
											? "Stored — leave empty to keep"
											: "Indexer API Key"
									}
									value={newsnabForm.api_key || ""}
									onChange={(e) => setNewsnabForm({ ...newsnabForm, api_key: e.target.value })}
									className="input input-sm w-full font-mono text-xs"
								/>
							</fieldset>

							<div className="grid grid-cols-2 gap-3">
								<fieldset className="fieldset">
									<legend className="fieldset-legend font-semibold text-xs">Priority Weight</legend>
									<input
										type="number"
										value={newsnabForm.weight ?? 0}
										onChange={(e) =>
											setNewsnabForm({ ...newsnabForm, weight: Number(e.target.value) })
										}
										className="input input-sm w-full font-mono text-xs"
									/>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend font-semibold text-xs">Timeout (sec)</legend>
									<input
										type="number"
										min="1"
										max="30"
										value={newsnabForm.timeout_seconds ?? 4}
										onChange={(e) =>
											setNewsnabForm({ ...newsnabForm, timeout_seconds: Number(e.target.value) })
										}
										className="input input-sm w-full font-mono text-xs"
									/>
								</fieldset>
							</div>

							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold text-xs">
									Categories (Newznab IDs)
								</legend>
								<div className="flex flex-wrap items-center gap-2">
									<input
										type="text"
										value={categoriesText}
										disabled={isReadOnly}
										onChange={(e) => {
											setCategoriesText(e.target.value);
											setCategoriesError(null);
										}}
										placeholder="Leave empty for automatic defaults"
										className={`input input-sm min-w-0 flex-1 font-mono text-xs ${
											categoriesError ? "input-error" : ""
										}`}
									/>
								</div>
								<div className="flex flex-wrap gap-1.5">
									{CATEGORY_PRESETS.map((preset) => (
										<button
											key={preset.label}
											type="button"
											disabled={isReadOnly}
											onClick={() => {
												setCategoriesText(preset.value);
												setCategoriesError(null);
											}}
											className="btn btn-outline btn-xs"
											title={preset.title}
										>
											{preset.label}
										</button>
									))}
									{categoriesText && (
										<button
											type="button"
											disabled={isReadOnly}
											onClick={() => {
												setCategoriesText("");
												setCategoriesError(null);
											}}
											className="btn btn-ghost btn-xs"
											title="Clear back to automatic per-type defaults"
										>
											Clear
										</button>
									)}
								</div>
								<p className="label text-base-content/60">
									{categoriesError ? (
										<span className="text-error">{categoriesError}</span>
									) : (
										CATEGORIES_HELP_TEXT
									)}
								</p>
							</fieldset>

							{newsnabTestStatus.message && (
								<div
									className={`alert alert-xs ${
										newsnabTestStatus.success ? "alert-success" : "alert-error"
									}`}
								>
									<span>{newsnabTestStatus.message}</span>
								</div>
							)}

							{newsnabTestStatus.success && discoveredCaps && (
								<div className="space-y-1.5 rounded-lg border border-base-300 bg-base-200/40 p-2.5">
									<span className="font-semibold text-[11px] text-base-content/70 uppercase tracking-wider">
										Discovered capabilities
									</span>
									<div className="flex flex-wrap gap-1.5">
										{discoveredCaps.tv_search !== false && (
											<span className="badge badge-success badge-xs">TV Search</span>
										)}
										{discoveredCaps.movie_search !== false && (
											<span className="badge badge-success badge-xs">Movies</span>
										)}
										{discoveredCaps.search !== false && (
											<span className="badge badge-ghost badge-xs">General</span>
										)}
										{typeof discoveredCaps.category_count === "number" && (
											<span className="badge badge-ghost badge-xs">
												{discoveredCaps.category_count} categories
											</span>
										)}
									</div>
									{(() => {
										const tv = discoveredCaps.tv_params ?? [];
										const movie = discoveredCaps.movie_params ?? [];
										const extras = [tv, movie]
											.flat()
											.filter((p, i, arr) => p !== "q" && p !== "cat" && arr.indexOf(p) === i);
										if (extras.length === 0) return null;
										return (
											<p className="text-[11px] text-base-content/60">
												Supported params: {extras.join(", ")}
											</p>
										);
									})()}
								</div>
							)}
						</div>

						<div className="modal-action flex items-center justify-between pt-2">
							<button
								type="button"
								disabled={newsnabTestStatus.loading || !newsnabForm.url || !newsnabForm.api_key}
								onClick={() =>
									handleTestNewsnab(
										newsnabForm.url || "",
										newsnabForm.api_key || "",
										editingNewsnab?.id,
									)
								}
								className="btn btn-outline btn-sm gap-1.5"
							>
								{newsnabTestStatus.loading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
								<span>Test Connection</span>
							</button>

							<div className="flex items-center gap-2">
								<button
									type="button"
									onClick={() => setIsNewsnabModalOpen(false)}
									className="btn btn-ghost btn-sm"
								>
									Cancel
								</button>
								<button
									type="button"
									onClick={handleSaveNewsnab}
									disabled={
										!newsnabForm.name ||
										!newsnabForm.url ||
										(!editingNewsnab && !newsnabForm.api_key) ||
										categoriesError !== null
									}
									className="btn btn-primary btn-sm"
								>
									Save Indexer
								</button>
							</div>
						</div>
					</div>
					<button
						type="button"
						className="modal-backdrop"
						onClick={() => setIsNewsnabModalOpen(false)}
					>
						Close
					</button>
				</dialog>
			)}
		</div>
	);
}
