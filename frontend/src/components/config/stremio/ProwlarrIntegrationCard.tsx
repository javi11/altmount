import { RefreshCw, Server, X } from "lucide-react";
import { useCallback, useState } from "react";
import type { ProwlarrConfig, ProwlarrIndexer } from "../../../types/config";
import { LoadingSpinner } from "../../ui/LoadingSpinner";

interface ProwlarrIntegrationCardProps {
	prowlarr: ProwlarrConfig;
	onChange: (updated: ProwlarrConfig) => void;
	isReadOnly?: boolean;
}

export function ProwlarrIntegrationCard({
	prowlarr,
	onChange,
	isReadOnly = false,
}: ProwlarrIntegrationCardProps) {
	const [categoryInput, setCategoryInput] = useState("");
	const [indexerNameInput, setIndexerNameInput] = useState("");
	const [indexers, setIndexers] = useState<ProwlarrIndexer[]>([]);
	const [loadingIndexers, setLoadingIndexers] = useState(false);
	const [indexersError, setIndexersError] = useState<string | null>(null);

	const categories = prowlarr.categories || [
		2000, 2010, 2030, 2040, 2045, 2060, 5000, 5010, 5030, 5040,
	];
	const preferredIndexerNames = prowlarr.preferred_indexer_names || [];

	const loadIndexers = useCallback(async () => {
		setLoadingIndexers(true);
		setIndexersError(null);
		try {
			const response = await fetch("/api/prowlarr/indexers", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					host: prowlarr.host || "",
					api_key: prowlarr.api_key || "",
				}),
			});
			const data = await response.json();
			if (data.success) {
				setIndexers((data.data as ProwlarrIndexer[]) ?? []);
			} else {
				setIndexersError(data.error?.message || data.error || "Failed to load indexers");
			}
		} catch (_error) {
			setIndexersError("Network error while loading indexers from Prowlarr");
		} finally {
			setLoadingIndexers(false);
		}
	}, [prowlarr.host, prowlarr.api_key]);

	const toggleIndexer = (id: number) => {
		const current = prowlarr.indexers ?? [];
		const next = current.includes(id) ? current.filter((i) => i !== id) : [...current, id];
		onChange({ ...prowlarr, indexers: next });
	};

	const handleAddCategory = () => {
		const num = Number.parseInt(categoryInput.trim(), 10);
		if (!Number.isNaN(num) && !categories.includes(num)) {
			onChange({ ...prowlarr, categories: [...categories, num] });
		}
		setCategoryInput("");
	};

	const handleRemoveCategory = (catId: number) => {
		onChange({ ...prowlarr, categories: categories.filter((c) => c !== catId) });
	};

	const handleAddIndexerName = () => {
		const name = indexerNameInput.trim();
		if (name && !preferredIndexerNames.includes(name)) {
			onChange({
				...prowlarr,
				preferred_indexer_names: [...preferredIndexerNames, name],
			});
		}
		setIndexerNameInput("");
	};

	const handleRemoveIndexerName = (name: string) => {
		onChange({
			...prowlarr,
			preferred_indexer_names: preferredIndexerNames.filter((n) => n !== name),
		});
	};

	return (
		<div className="min-w-0 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 shadow-sm">
			{/* Section Header */}
			<div className="flex flex-wrap items-center justify-between gap-4 border-base-300/50 border-b pb-4">
				<div className="flex items-center gap-2.5">
					<div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
						<Server className="h-4 w-4" />
					</div>
					<div>
						<h3 className="font-bold text-base tracking-tight">Prowlarr Indexer Integration</h3>
						<p className="text-base-content/60 text-xs">
							Connect to Prowlarr to automatically search Usenet indexers by IMDb ID
						</p>
					</div>
				</div>

				<div className="flex items-center gap-3">
					<span className="text-base-content/70 text-xs">Enable Prowlarr Search</span>
					<input
						type="checkbox"
						className="toggle toggle-primary toggle-sm"
						checked={prowlarr.enabled ?? false}
						disabled={isReadOnly}
						onChange={(e) => onChange({ ...prowlarr, enabled: e.target.checked })}
					/>
				</div>
			</div>

			{prowlarr.enabled && (
				<div className="fade-in slide-in-from-top-2 mt-5 animate-in space-y-6">
					{/* Prowlarr Host & API Key */}
					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
						<fieldset className="fieldset min-w-0">
							<legend className="fieldset-legend font-semibold text-xs">Prowlarr Host</legend>
							<input
								type="url"
								className="input input-bordered w-full font-mono text-xs"
								placeholder="http://localhost:9696"
								value={prowlarr.host ?? ""}
								disabled={isReadOnly}
								onChange={(e) => onChange({ ...prowlarr, host: e.target.value })}
							/>
						</fieldset>

						<fieldset className="fieldset min-w-0">
							<legend className="fieldset-legend font-semibold text-xs">API Key</legend>
							<input
								type="password"
								className="input input-bordered w-full font-mono text-xs"
								placeholder="Prowlarr API key"
								value={prowlarr.api_key ?? ""}
								disabled={isReadOnly}
								onChange={(e) => onChange({ ...prowlarr, api_key: e.target.value })}
							/>
						</fieldset>
					</div>

					{/* Newznab Categories */}
					<div className="space-y-2">
						<div className="font-semibold text-base-content/70 text-xs">Newznab Category IDs</div>
						<div className="flex min-h-10 min-w-0 flex-wrap items-center gap-1.5 rounded-xl border border-base-300 bg-base-100/90 p-2 shadow-inner">
							{categories.map((catId) => (
								<span key={catId} className="badge badge-neutral gap-1 text-xs">
									<span>{catId}</span>
									{!isReadOnly && (
										<button
											type="button"
											onClick={() => handleRemoveCategory(catId)}
											className="hover:opacity-75"
											aria-label={`Remove category ${catId}`}
										>
											<X className="h-3 w-3" />
										</button>
									)}
								</span>
							))}

							{!isReadOnly && (
								<input
									type="text"
									className="input input-ghost input-xs w-24 text-xs focus:outline-none"
									placeholder="Add ID + Enter"
									value={categoryInput}
									onChange={(e) => setCategoryInput(e.target.value)}
									onKeyDown={(e) => {
										if (e.key === "Enter" || e.key === ",") {
											e.preventDefault();
											handleAddCategory();
										}
									}}
									onBlur={handleAddCategory}
								/>
							)}
						</div>
						<p className="text-[11px] text-base-content/50">
							Standard Newznab categories: 2000, 2010, 2030, 2040, 2045, 2060 (Movies) and 5000,
							5010, 5030, 5040 (TV).
						</p>
					</div>

					{/* Indexer Selection */}
					<div className="space-y-3">
						<div className="flex flex-wrap items-center justify-between gap-2">
							<div className="font-semibold text-base-content/70 text-xs">Active Indexers</div>

							<div className="flex items-center gap-2">
								<button
									type="button"
									className="btn btn-outline btn-xs gap-1.5"
									onClick={loadIndexers}
									disabled={isReadOnly || loadingIndexers || !prowlarr.host}
								>
									{loadingIndexers ? (
										<LoadingSpinner size="sm" />
									) : (
										<RefreshCw className="h-3.5 w-3.5" />
									)}
									<span>{loadingIndexers ? "Loading..." : "Fetch from Prowlarr"}</span>
								</button>
								{(prowlarr.indexers?.length ?? 0) > 0 && (
									<span className="badge badge-primary badge-xs">
										{prowlarr.indexers?.length} selected
									</span>
								)}
							</div>
						</div>

						{indexersError && (
							<div className="alert alert-error py-2 text-xs">
								<X className="h-4 w-4" />
								<span>{indexersError}</span>
							</div>
						)}

						{indexers.length > 0 && (
							<div className="max-h-56 overflow-y-auto rounded-xl border border-base-300 bg-base-100/90 p-2 shadow-inner">
								<div className="grid grid-cols-1 gap-1 sm:grid-cols-2">
									{indexers.map((idx) => {
										const selected = prowlarr.indexers?.includes(idx.id) ?? false;
										return (
											<label
												key={idx.id}
												className="flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-1.5 hover:bg-base-200"
											>
												<input
													type="checkbox"
													className="checkbox checkbox-xs checkbox-primary"
													checked={selected}
													disabled={isReadOnly}
													onChange={() => toggleIndexer(idx.id)}
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
						<p className="text-[11px] text-base-content/50">
							Leave all unchecked to search across all configured Usenet indexers in Prowlarr.
						</p>
					</div>

					{/* Preferred Indexer Names */}
					<div className="space-y-2">
						<div className="font-semibold text-base-content/70 text-xs">
							Preferred Indexer Names (Top Rank Priority)
						</div>
						<div className="flex min-h-10 min-w-0 flex-wrap items-center gap-1.5 rounded-xl border border-base-300 bg-base-100/90 p-2 shadow-inner">
							{preferredIndexerNames.map((name) => (
								<span key={name} className="badge badge-primary gap-1 text-xs">
									<span>{name}</span>
									{!isReadOnly && (
										<button
											type="button"
											onClick={() => handleRemoveIndexerName(name)}
											className="hover:opacity-75"
											aria-label={`Remove ${name}`}
										>
											<X className="h-3 w-3" />
										</button>
									)}
								</span>
							))}

							{!isReadOnly && (
								<input
									type="text"
									className="input input-ghost input-xs min-w-32 flex-1 text-xs focus:outline-none"
									placeholder="e.g. NZBGeek, DrunkenSlug + Enter"
									value={indexerNameInput}
									onChange={(e) => setIndexerNameInput(e.target.value)}
									onKeyDown={(e) => {
										if (e.key === "Enter" || e.key === ",") {
											e.preventDefault();
											handleAddIndexerName();
										}
									}}
									onBlur={handleAddIndexerName}
								/>
							)}
						</div>
						<p className="text-[11px] text-base-content/50">
							Indexers matching these names will receive rank bonus points (+10,000 pts) and be
							ordered at the top.
						</p>
					</div>
				</div>
			)}
		</div>
	);
}
