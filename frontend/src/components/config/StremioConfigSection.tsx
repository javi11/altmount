import { Check, Copy, ExternalLink, Info, Save, Tv, X } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, ProwlarrConfig, StremioConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface StremioConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: StremioConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

const DEFAULT_PROWLARR: ProwlarrConfig = {
	enabled: false,
	host: "http://localhost:9696",
	api_key: "",
	categories: [2000, 2010, 2030, 2040, 2045, 2060, 5000, 5010, 5030, 5040],
	languages: [],
	qualities: [],
};

const resolveProwlarr = (p: ProwlarrConfig | undefined): ProwlarrConfig => {
	const base = p ?? DEFAULT_PROWLARR;
	return {
		...base,
		categories: base.categories?.length ? base.categories : DEFAULT_PROWLARR.categories,
	};
};

export function StremioConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: StremioConfigSectionProps) {
	const [formData, setFormData] = useState<StremioConfig>({
		enabled: config.stremio?.enabled ?? false,
		nzb_ttl_hours: config.stremio?.nzb_ttl_hours ?? 24,
		base_url: config.stremio?.base_url ?? "",
		prowlarr: resolveProwlarr(config.stremio?.prowlarr),
	});
	const [hasChanges, setHasChanges] = useState(false);
	const [urlCopied, setUrlCopied] = useState(false);
	const [newCat, setNewCat] = useState("");
	const [newLang, setNewLang] = useState("");
	const [newQual, setNewQual] = useState("");

	useEffect(() => {
		setFormData({
			enabled: config.stremio?.enabled ?? false,
			nzb_ttl_hours: config.stremio?.nzb_ttl_hours ?? 24,
			base_url: config.stremio?.base_url ?? "",
			prowlarr: resolveProwlarr(config.stremio?.prowlarr),
		});
		setHasChanges(false);
	}, [config.stremio]);

	const markChanged = (updated: StremioConfig) => {
		const orig = config.stremio;
		const changed =
			updated.enabled !== (orig?.enabled ?? false) ||
			updated.nzb_ttl_hours !== (orig?.nzb_ttl_hours ?? 24) ||
			updated.base_url !== (orig?.base_url ?? "") ||
			JSON.stringify(updated.prowlarr) !== JSON.stringify(orig?.prowlarr ?? DEFAULT_PROWLARR);
		setHasChanges(changed);
	};

	const update = (patch: Partial<StremioConfig>) => {
		const updated = { ...formData, ...patch };
		setFormData(updated);
		markChanged(updated);
	};

	const updateProwlarr = (patch: Partial<ProwlarrConfig>) => {
		const updated = { ...formData, prowlarr: { ...formData.prowlarr, ...patch } };
		setFormData(updated);
		markChanged(updated);
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("stremio", formData);
			setHasChanges(false);
		}
	};

	const addonURL =
		formData.enabled && config.download_key
			? `${(formData.base_url || "").replace(/\/$/, "") || window.location.origin}/stremio/${config.download_key}/manifest.json`
			: null;

	const handleCopyURL = async () => {
		if (!addonURL) return;
		await navigator.clipboard.writeText(addonURL);
		setUrlCopied(true);
		setTimeout(() => setUrlCopied(false), 2000);
	};

	const handleInstallInStremio = () => {
		if (!addonURL) return;
		window.open(`stremio://${addonURL.replace(/^https?:\/\//, "")}`, "_blank");
	};

	return (
		<div className="space-y-10">
			<div>
				<h3 className="font-bold text-base-content text-lg tracking-tight">Stremio Integration</h3>
				<p className="break-words text-base-content/50 text-sm">
					Enable the Stremio addon to automatically search Prowlarr for NZBs by IMDB ID and stream
					them directly from Stremio.
				</p>
			</div>

			<div className="space-y-8">
				{/* Enable / Disable */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Tv className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Endpoint
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="flex items-center justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h5 className="break-words font-bold text-sm">Enable Stremio Integration</h5>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Activates the Stremio addon endpoints and the{" "}
								<code className="rounded bg-base-300 px-1 py-0.5 font-mono text-[10px]">
									POST /api/nzb/streams
								</code>{" "}
								NZB upload endpoint.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary mt-1 shrink-0"
							checked={formData.enabled}
							disabled={isReadOnly}
							onChange={(e) => update({ enabled: e.target.checked })}
						/>
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Public Base URL</legend>
						<input
							type="url"
							className="input w-full"
							placeholder="https://altmount.example.com"
							value={formData.base_url ?? ""}
							disabled={isReadOnly}
							onChange={(e) => update({ base_url: e.target.value })}
						/>
						<p className="label">
							Public base URL used when building stream links. Leave empty to auto-detect from the
							request.
						</p>
					</fieldset>
				</div>

				{/* Addon URL */}
				{addonURL && (
					<div className="space-y-4 rounded-2xl border-2 border-primary/30 bg-primary/5 p-6">
						<div className="flex items-center gap-2">
							<Tv className="h-4 w-4 text-primary" />
							<h4 className="font-bold text-primary text-xs uppercase tracking-widest">
								Addon Install URL
							</h4>
							<div className="h-px flex-1 bg-primary/20" />
						</div>
						<p className="break-words text-base-content/60 text-xs">
							Install this URL in Stremio to enable automatic Usenet streaming via Prowlarr.
						</p>
						<div className="flex items-center gap-2">
							<code className="min-w-0 flex-1 truncate rounded-lg bg-base-300 px-3 py-2 font-mono text-[11px]">
								{addonURL}
							</code>
							<button
								type="button"
								className="btn btn-sm btn-ghost shrink-0"
								onClick={handleCopyURL}
								title="Copy URL"
							>
								{urlCopied ? (
									<Check className="h-4 w-4 text-success" />
								) : (
									<Copy className="h-4 w-4" />
								)}
							</button>
							<button
								type="button"
								className="btn btn-sm btn-primary shrink-0"
								onClick={handleInstallInStremio}
								title="Install in Stremio"
							>
								<ExternalLink className="h-4 w-4" />
								Install
							</button>
						</div>
					</div>
				)}

				{/* Cache TTL */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Info className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Cache
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">NZB File Cache TTL (hours)</legend>
						<input
							type="number"
							className="input w-32"
							min={0}
							value={formData.nzb_ttl_hours}
							disabled={isReadOnly}
							onChange={(e) => update({ nzb_ttl_hours: Math.max(0, Number(e.target.value)) })}
						/>
						<p className="label">
							How long AltMount keeps the cached NZB/meta file on disk. Set to <strong>0</strong> to
							never delete.
						</p>
					</fieldset>
				</div>

				{/* Prowlarr */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Tv className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Prowlarr Indexer
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="flex items-center justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h5 className="break-words font-bold text-sm">Enable Prowlarr Search</h5>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								When enabled, the Stremio addon automatically searches Prowlarr for NZBs by IMDB ID
								and queues the best result.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary mt-1 shrink-0"
							checked={formData.prowlarr?.enabled ?? false}
							disabled={isReadOnly}
							onChange={(e) => updateProwlarr({ enabled: e.target.checked })}
						/>
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Prowlarr Host</legend>
						<input
							type="url"
							className="input w-full"
							placeholder="http://localhost:9696"
							value={formData.prowlarr?.host ?? ""}
							disabled={isReadOnly}
							onChange={(e) => updateProwlarr({ host: e.target.value })}
						/>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">API Key</legend>
						<input
							type="password"
							className="input w-full"
							placeholder="Prowlarr API key"
							value={formData.prowlarr?.api_key ?? ""}
							disabled={isReadOnly}
							onChange={(e) => updateProwlarr({ api_key: e.target.value })}
						/>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Categories</legend>
						<div className="flex min-h-10 flex-wrap gap-2 rounded-box border border-base-300 bg-base-100 p-2">
							{(formData.prowlarr?.categories ?? []).map((cat) => (
								<span key={cat} className="badge badge-neutral gap-1">
									{cat}
									{!isReadOnly && (
										<button
											type="button"
											aria-label={`Remove category ${cat}`}
											onClick={() =>
												updateProwlarr({
													categories: (formData.prowlarr?.categories ?? []).filter(
														(c) => c !== cat,
													),
												})
											}
										>
											<X className="h-3 w-3" />
										</button>
									)}
								</span>
							))}
							{!isReadOnly && (
								<input
									type="number"
									className="input input-ghost input-xs w-24 min-w-0 focus:outline-none"
									placeholder="Add ID…"
									value={newCat}
									onChange={(e) => setNewCat(e.target.value)}
									onKeyDown={(e) => {
										if (e.key === "Enter" || e.key === ",") {
											e.preventDefault();
											const n = Number.parseInt(newCat.trim(), 10);
											if (!Number.isNaN(n) && !(formData.prowlarr?.categories ?? []).includes(n)) {
												updateProwlarr({
													categories: [...(formData.prowlarr?.categories ?? []), n],
												});
											}
											setNewCat("");
										}
									}}
									onBlur={() => {
										const n = Number.parseInt(newCat.trim(), 10);
										if (!Number.isNaN(n) && !(formData.prowlarr?.categories ?? []).includes(n)) {
											updateProwlarr({ categories: [...(formData.prowlarr?.categories ?? []), n] });
										}
										setNewCat("");
									}}
								/>
							)}
						</div>
						<p className="label">
							Newznab category IDs. Press Enter or comma to add. Defaults: 2000 (Movies), 2040
							(Movies/HD), 2060 (Movies/4K), 5000 (TV), 5040 (TV/HD).
						</p>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Language Filter</legend>
						<div className="flex min-h-10 flex-wrap gap-2 rounded-box border border-base-300 bg-base-100 p-2">
							{(formData.prowlarr?.languages ?? []).map((lang) => (
								<span key={lang} className="badge badge-neutral gap-1">
									{lang}
									{!isReadOnly && (
										<button
											type="button"
											aria-label={`Remove ${lang}`}
											onClick={() =>
												updateProwlarr({
													languages: (formData.prowlarr?.languages ?? []).filter((l) => l !== lang),
												})
											}
										>
											<X className="h-3 w-3" />
										</button>
									)}
								</span>
							))}
							{!isReadOnly && (
								<input
									type="text"
									className="input input-ghost input-xs w-28 min-w-0 focus:outline-none"
									placeholder="Add keyword…"
									value={newLang}
									onChange={(e) => setNewLang(e.target.value)}
									onKeyDown={(e) => {
										if (e.key === "Enter" || e.key === ",") {
											e.preventDefault();
											const v = newLang.trim();
											if (v && !(formData.prowlarr?.languages ?? []).includes(v)) {
												updateProwlarr({
													languages: [...(formData.prowlarr?.languages ?? []), v],
												});
											}
											setNewLang("");
										}
									}}
									onBlur={() => {
										const v = newLang.trim();
										if (v && !(formData.prowlarr?.languages ?? []).includes(v)) {
											updateProwlarr({
												languages: [...(formData.prowlarr?.languages ?? []), v],
											});
										}
										setNewLang("");
									}}
								/>
							)}
						</div>
						<p className="label">
							Only show releases whose title contains at least one of these keywords. Leave empty to
							show all languages.
						</p>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Quality Filter</legend>
						<div className="flex min-h-10 flex-wrap gap-2 rounded-box border border-base-300 bg-base-100 p-2">
							{(formData.prowlarr?.qualities ?? []).map((qual) => (
								<span key={qual} className="badge badge-neutral gap-1">
									{qual}
									{!isReadOnly && (
										<button
											type="button"
											aria-label={`Remove ${qual}`}
											onClick={() =>
												updateProwlarr({
													qualities: (formData.prowlarr?.qualities ?? []).filter((q) => q !== qual),
												})
											}
										>
											<X className="h-3 w-3" />
										</button>
									)}
								</span>
							))}
							{!isReadOnly && (
								<input
									type="text"
									className="input input-ghost input-xs w-28 min-w-0 focus:outline-none"
									placeholder="Add keyword…"
									value={newQual}
									onChange={(e) => setNewQual(e.target.value)}
									onKeyDown={(e) => {
										if (e.key === "Enter" || e.key === ",") {
											e.preventDefault();
											const v = newQual.trim();
											if (v && !(formData.prowlarr?.qualities ?? []).includes(v)) {
												updateProwlarr({
													qualities: [...(formData.prowlarr?.qualities ?? []), v],
												});
											}
											setNewQual("");
										}
									}}
									onBlur={() => {
										const v = newQual.trim();
										if (v && !(formData.prowlarr?.qualities ?? []).includes(v)) {
											updateProwlarr({
												qualities: [...(formData.prowlarr?.qualities ?? []), v],
											});
										}
										setNewQual("");
									}}
								/>
							)}
						</div>
						<p className="label">
							Only show releases whose title contains at least one of these keywords. Leave empty to
							show all quality tiers.
						</p>
					</fieldset>
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end border-base-200 border-t pt-4">
					<button
						type="button"
						className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${!hasChanges && "btn-ghost border-base-300"}`}
						onClick={handleSave}
						disabled={!hasChanges || isUpdating}
					>
						{isUpdating ? <LoadingSpinner size="sm" /> : <Save className="h-4 w-4" />}
						{isUpdating ? "Saving..." : "Save Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
