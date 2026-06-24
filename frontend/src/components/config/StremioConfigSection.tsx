import {
	AlertTriangle,
	Check,
	CheckCircle2,
	ChevronDown,
	Copy,
	ExternalLink,
	Save,
	Tv,
	X,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { copyToClipboard } from "../../lib/utils";
import { apiClient } from "../../api/client";
import type { ConfigResponse, ProwlarrConfig, StremioConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface TagInputProps {
	tags: string[];
	onChange: (tags: string[]) => void;
	disabled?: boolean;
	placeholder?: string;
	parseValue?: (raw: string) => string | null;
}

function TagInput({
	tags,
	onChange,
	disabled = false,
	placeholder = "Add...",
	parseValue,
}: TagInputProps) {
	const [inputValue, setInputValue] = useState("");

	const addTag = useCallback(
		(raw: string) => {
			const value = parseValue ? parseValue(raw) : raw.trim();
			if (value && !tags.includes(value)) {
				onChange([...tags, value]);
			}
		},
		[tags, onChange, parseValue],
	);

	const commitAndClear = useCallback(() => {
		addTag(inputValue);
		setInputValue("");
	}, [inputValue, addTag]);

	return (
		<div className="flex min-h-10 min-w-0 flex-wrap gap-2 rounded-box border border-base-300 bg-base-100 p-2">
			{tags.map((tag) => (
				<span key={String(tag)} className="badge badge-neutral gap-1">
					{String(tag)}
					{!disabled && (
						<button
							type="button"
							aria-label={`Remove ${tag}`}
							onClick={() => onChange(tags.filter((t) => t !== tag))}
						>
							<X className="h-3 w-3" />
						</button>
					)}
				</span>
			))}
			{!disabled && (
				<input
					type="text"
					className="input input-ghost input-xs w-28 min-w-0 focus:outline-none"
					placeholder={placeholder}
					value={inputValue}
					onChange={(e) => setInputValue(e.target.value)}
					onKeyDown={(e) => {
						if (e.key === "Enter" || e.key === ",") {
							e.preventDefault();
							commitAndClear();
						}
					}}
					onBlur={commitAndClear}
				/>
			)}
		</div>
	);
}

interface StremioConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: StremioConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

const DEFAULT_PROWLARR: ProwlarrConfig = {
	deprecated: true,
	enabled: false,
	host: "http://localhost:9696",
	api_key: "",
	categories: [2000, 2010, 2030, 2040, 2045, 2060, 5000, 5010, 5030, 5040],
	languages: [],
	qualities: [],
};

function resolveProwlarr(p: ProwlarrConfig | undefined): ProwlarrConfig {
	const base = p ?? DEFAULT_PROWLARR;
	return {
		...base,
		deprecated: true,
		categories: base.categories?.length ? base.categories : DEFAULT_PROWLARR.categories,
	};
}

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
	const [addonCopied, setAddonCopied] = useState(false);

	useEffect(() => {
		setFormData({
			enabled: config.stremio?.enabled ?? false,
			nzb_ttl_hours: config.stremio?.nzb_ttl_hours ?? 24,
			base_url: config.stremio?.base_url ?? "",
			prowlarr: resolveProwlarr(config.stremio?.prowlarr),
		});
		setHasChanges(false);
	}, [config.stremio]);

	const { data: setup } = useQuery({
		queryKey: ["stremio-setup"],
		queryFn: () => apiClient.getStremioSetup(),
		enabled: config.stremio?.enabled === true,
		retry: false,
	});

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

	// Legacy per-user Prowlarr addon URL
	const addonURL =
		formData.enabled && config.download_key
			? `${(formData.base_url || "").replace(/\/$/, "") || window.location.origin}/stremio/${config.download_key}/manifest.json`
			: null;

	const handleCopyURL = async () => {
		if (!addonURL) return;
		const ok = await copyToClipboard(addonURL);
		if (ok) {
			setUrlCopied(true);
			setTimeout(() => setUrlCopied(false), 2000);
		}
	};

	const handleInstallInStremio = () => {
		if (!addonURL) return;
		window.open(`stremio://${addonURL.replace(/^https?:\/\//, "")}`, "_blank");
	};

	const handleCopyAddonURL = async () => {
		if (!setup?.aiostreams_addon_url) return;
		const ok = await copyToClipboard(setup.aiostreams_addon_url);
		if (ok) {
			setAddonCopied(true);
			setTimeout(() => setAddonCopied(false), 2000);
		}
	};

	const aioStreamsInstallURL = setup?.aiostreams_addon_url
		? `stremio://${setup.aiostreams_addon_url.replace(/^https?:\/\//, "")}`
		: null;

	const aioStreamsUIURL =
		setup?.aiostreams_ui_url ??
		`${window.location.protocol}//${window.location.hostname}:8081`;

	return (
		<div className="min-w-0 space-y-10">
			<div className="min-w-0 space-y-8">
				{/* ── Enable / Base Config ─────────────────────────────────── */}
				<div className="min-w-0 space-y-6 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
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

					<div className="grid min-w-0 grid-cols-1 gap-6 sm:grid-cols-2">
						<fieldset className="fieldset min-w-0">
							<legend className="fieldset-legend">Public Base URL</legend>
							<input
								type="url"
								className="input w-full min-w-0 max-w-full"
								placeholder="https://altmount.example.com"
								value={formData.base_url ?? ""}
								disabled={isReadOnly}
								onChange={(e) => update({ base_url: e.target.value })}
							/>
							<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
								Base address for generated stream links. Leave empty to auto-detect.
							</p>
						</fieldset>

						<fieldset className="fieldset min-w-0">
							<legend className="fieldset-legend">NZB Cache TTL (hours)</legend>
							<input
								type="number"
								className="input w-full min-w-0 max-w-full"
								min={0}
								value={formData.nzb_ttl_hours}
								disabled={isReadOnly}
								onChange={(e) => update({ nzb_ttl_hours: Math.max(0, Number(e.target.value)) })}
							/>
							<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
								How long cached NZB files stay on disk. Use <strong>0</strong> to keep forever.
							</p>
						</fieldset>
					</div>
				</div>

				{/* ── AIOStreams — recommended (shown when Stremio is enabled) ── */}
				{formData.enabled && (
					<div className="min-w-0 overflow-hidden rounded-2xl border-2 border-success/40 bg-success/5 p-6 space-y-5">
						<div className="flex items-center gap-2">
							<CheckCircle2 className="h-4 w-4 text-success" />
							<h4 className="font-bold text-success text-xs uppercase tracking-widest">
								AIOStreams
							</h4>
							<span className="badge badge-success badge-sm">Recommended</span>
							<div className="h-px flex-1 bg-success/20" />
						</div>

						<p className="text-sm text-base-content/70">
							AIOStreams handles NZB indexer search. AltMount handles NZB download and
							streaming. Your NNTP providers stay in AltMount — nothing moves.
						</p>

						{/* AIOStreams UI link */}
						<div className="flex items-center justify-between rounded-xl bg-base-200 px-4 py-3">
							<div className="min-w-0 flex-1">
								<div className="text-sm font-medium">AIOStreams UI</div>
								<div className="text-xs text-base-content/60 font-mono truncate">
									{aioStreamsUIURL}
								</div>
							</div>
							<a
								href={aioStreamsUIURL}
								target="_blank"
								rel="noopener noreferrer"
								className="btn btn-sm btn-outline gap-1 shrink-0 ml-3"
							>
								Open <ExternalLink className="h-3.5 w-3.5" />
							</a>
						</div>

						{/* Step-by-step guide */}
						<details className="group">
							<summary className="flex cursor-pointer items-center gap-2 text-sm font-medium select-none list-none">
								<ChevronDown className="h-4 w-4 transition-transform group-open:rotate-180" />
								Setup guide — 3 steps
							</summary>

							<ol className="mt-4 space-y-5">
								<li className="flex gap-3">
									<span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary text-primary-content text-xs font-bold">
										1
									</span>
									<div className="min-w-0">
										<div className="text-sm font-medium">
											Start AIOStreams alongside AltMount
										</div>
										<div className="text-xs text-base-content/60 mt-1">
											Run the following from your AltMount directory. AIOStreams will boot
											already wired to this instance — no URL or key configuration needed.
										</div>
										<code className="block mt-2 rounded-lg bg-base-200 px-3 py-2 text-xs font-mono break-all">
											docker compose -f docker-compose.yml -f docker-compose.stremio.yml up -d
										</code>
									</div>
								</li>

								<li className="flex gap-3">
									<span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary text-primary-content text-xs font-bold">
										2
									</span>
									<div className="min-w-0">
										<div className="text-sm font-medium">
											Open AIOStreams and add your NZB indexers
										</div>
										<div className="text-xs text-base-content/60 mt-1">
											Configure Prowlarr, NZBgeek, or any other indexer in the AIOStreams UI
											(port 8081). AltMount is already set as the NZB download backend.
										</div>
									</div>
								</li>

								<li className="flex gap-3">
									<span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary text-primary-content text-xs font-bold">
										3
									</span>
									<div className="min-w-0 flex-1 space-y-2">
										<div className="text-sm font-medium">
											Install the AIOStreams addon in Stremio
										</div>
										{setup?.aiostreams_addon_url ? (
											<>
												<div className="flex items-center gap-2">
													<code className="flex-1 truncate rounded-lg bg-base-200 px-2 py-1 text-xs font-mono">
														{setup.aiostreams_addon_url}
													</code>
													<button
														type="button"
														className="btn btn-xs btn-ghost shrink-0"
														onClick={handleCopyAddonURL}
														title="Copy addon URL"
													>
														{addonCopied ? (
															<CheckCircle2 className="h-3.5 w-3.5 text-success" />
														) : (
															<Copy className="h-3.5 w-3.5" />
														)}
													</button>
												</div>
												{aioStreamsInstallURL && (
													<a
														href={aioStreamsInstallURL}
														className="btn btn-sm btn-primary gap-1"
													>
														Install in Stremio{" "}
														<ExternalLink className="h-3.5 w-3.5" />
													</a>
												)}
											</>
										) : (
											<div className="text-xs text-base-content/40">
												Start AIOStreams first to generate your addon URL.
											</div>
										)}
									</div>
								</li>
							</ol>
						</details>
					</div>
				)}

				{/* ── Prowlarr — deprecated (collapsible) ─────────────────── */}
				<details className="group">
					<summary className="flex cursor-pointer items-center gap-2 select-none list-none rounded-xl border-2 border-warning/30 bg-warning/5 px-4 py-3">
						<ChevronDown className="h-4 w-4 transition-transform group-open:rotate-180 shrink-0" />
						<AlertTriangle className="h-4 w-4 text-warning shrink-0" />
						<span className="font-medium text-sm flex-1">Prowlarr integration</span>
						<span className="badge badge-warning badge-sm">Deprecated</span>
					</summary>

					<div className="mt-3 min-w-0 space-y-6 overflow-hidden rounded-2xl border-2 border-warning/30 bg-base-200/60 p-6">
						<div className="alert alert-warning py-2 text-sm">
							<AlertTriangle className="h-4 w-4 shrink-0" />
							<div>
								The built-in Prowlarr integration is deprecated. Migrate to AIOStreams above
								— it supports Prowlarr and other indexers with better filtering.
							</div>
						</div>

						<div className="flex items-center justify-between gap-4">
							<div className="min-w-0 flex-1">
								<h5 className="break-words font-bold text-sm">Enable Prowlarr Search</h5>
								<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
									When enabled, the Stremio addon automatically searches Prowlarr for NZBs
									by IMDB ID and queues the best result.
								</p>
							</div>
							<input
								type="checkbox"
								className="toggle toggle-warning mt-1 shrink-0"
								checked={formData.prowlarr?.enabled ?? false}
								disabled={isReadOnly}
								onChange={(e) => updateProwlarr({ enabled: e.target.checked })}
							/>
						</div>

						{formData.prowlarr?.enabled && (
							<div className="fade-in slide-in-from-top-2 animate-in space-y-6 border-base-300/50 border-t pt-6">
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend">Prowlarr Host</legend>
									<input
										type="url"
										className="input w-full min-w-0 max-w-full"
										placeholder="http://localhost:9696"
										value={formData.prowlarr?.host ?? ""}
										disabled={isReadOnly}
										onChange={(e) => updateProwlarr({ host: e.target.value })}
									/>
								</fieldset>

								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend">API Key</legend>
									<input
										type="password"
										className="input w-full min-w-0 max-w-full"
										placeholder="Prowlarr API key"
										value={formData.prowlarr?.api_key ?? ""}
										disabled={isReadOnly}
										onChange={(e) => updateProwlarr({ api_key: e.target.value })}
									/>
								</fieldset>

								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend">Categories</legend>
									<TagInput
										tags={(formData.prowlarr?.categories ?? []).map(String)}
										onChange={(tags) =>
											updateProwlarr({
												categories: tags.map((t) => Number.parseInt(t, 10)),
											})
										}
										disabled={isReadOnly}
										placeholder="Add ID..."
										parseValue={(raw) => {
											const n = Number.parseInt(raw.trim(), 10);
											return Number.isNaN(n) ? null : String(n);
										}}
									/>
									<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
										Newznab category IDs. Press Enter or comma to add. Defaults: 2000
										(Movies), 2040 (Movies/HD), 2060 (Movies/4K), 5000 (TV), 5040 (TV/HD).
									</p>
								</fieldset>

								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend">Language Filter</legend>
									<TagInput
										tags={formData.prowlarr?.languages ?? []}
										onChange={(languages) => updateProwlarr({ languages })}
										disabled={isReadOnly}
										placeholder="Add keyword..."
									/>
									<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
										Only show releases whose title contains at least one of these keywords.
										Leave empty to show all languages.
									</p>
								</fieldset>

								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend">Quality Filter</legend>
									<TagInput
										tags={formData.prowlarr?.qualities ?? []}
										onChange={(qualities) => updateProwlarr({ qualities })}
										disabled={isReadOnly}
										placeholder="Add keyword..."
									/>
									<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
										Only show releases whose title contains at least one of these keywords.
										Leave empty to show all quality tiers.
									</p>
								</fieldset>
							</div>
						)}

						{/* Legacy Prowlarr addon URL */}
						{addonURL && (
							<div className="min-w-0 space-y-3 rounded-xl border border-base-300 bg-base-100 p-4">
								<p className="text-xs text-base-content/50">
									Legacy Prowlarr addon URL — use the AIOStreams addon URL above instead.
								</p>
								<div className="flex min-w-0 flex-wrap items-center gap-2">
									<code className="min-w-0 flex-1 basis-0 truncate rounded-lg bg-base-300 px-3 py-2 font-mono text-[11px]">
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
										className="btn btn-sm btn-outline shrink-0"
										onClick={handleInstallInStremio}
										title="Install in Stremio"
									>
										<ExternalLink className="h-4 w-4" />
										Install
									</button>
								</div>
							</div>
						)}
					</div>
				</details>
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
