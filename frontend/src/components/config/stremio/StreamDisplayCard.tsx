import { AlertTriangle, LayoutList, Zap } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { apiClient } from "../../../api/client";
import type {
	StremioConfig,
	StremioDisplayConfig,
	StremioDisplayPreset,
	StremioDisplayPresetId,
	StremioDisplayPreview,
} from "../../../types/config";

interface StreamDisplayCardProps {
	config: StremioConfig;
	onChange: (patch: Partial<StremioConfig>) => void;
	isReadOnly?: boolean;
}

export function StreamDisplayCard({
	config,
	onChange,
	isReadOnly = false,
}: StreamDisplayCardProps) {
	const display: StremioDisplayConfig = config.display ?? { preset: "default" };
	const preset: StremioDisplayPresetId = display.preset ?? "default";

	const [presets, setPresets] = useState<StremioDisplayPreset[]>([]);
	const [tokens, setTokens] = useState<string[]>([]);
	const [previews, setPreviews] = useState<StremioDisplayPreview[]>([]);
	const [previewError, setPreviewError] = useState<string | null>(null);
	const previewSeqRef = useRef(0);

	useEffect(() => {
		apiClient
			.getStremioDisplayPresets()
			.then((res) => {
				setPresets(res.presets);
				setTokens(res.tokens);
			})
			.catch((err) =>
				setPreviewError(err instanceof Error ? err.message : "Failed to load presets"),
			);
	}, []);

	// Previews are rendered by the backend with the same code that labels
	// streams for Stremio, so what you see here is what Stremio shows. Keyed
	// on the serialised config so a fresh-but-equal object doesn't refetch.
	const displayKey = JSON.stringify(display);
	const showCachedIndicator = config.show_cached_indicator;
	useEffect(() => {
		const seq = ++previewSeqRef.current;
		const timer = setTimeout(async () => {
			try {
				const res = await apiClient.previewStremioDisplay(
					JSON.parse(displayKey) as StremioDisplayConfig,
					showCachedIndicator,
				);
				if (seq !== previewSeqRef.current) return;
				setPreviews(res);
				setPreviewError(null);
			} catch (err) {
				if (seq !== previewSeqRef.current) return;
				setPreviewError(err instanceof Error ? err.message : "Failed to render preview");
			}
		}, 250);
		return () => clearTimeout(timer);
	}, [displayKey, showCachedIndicator]);

	const updateDisplay = (patch: Partial<StremioDisplayConfig>) =>
		onChange({ display: { ...display, ...patch } });

	const selectPreset = (id: StremioDisplayPresetId) => {
		if (id !== "custom") {
			updateDisplay({ preset: id });
			return;
		}
		// Start a custom format from the one currently shown.
		const base = presets.find((p) => p.id === preset) ?? presets[0];
		updateDisplay({
			preset: "custom",
			name_template: display.name_template || base?.name_template || "",
			description_template: display.description_template || base?.description_template || "",
		});
	};

	const options: { id: StremioDisplayPresetId; label: string }[] = [
		...presets.map((p) => ({ id: p.id, label: p.label })),
		{ id: "custom", label: "Custom" },
	];

	return (
		<div className="min-w-0 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 shadow-sm">
			<div className="flex items-center gap-2.5 border-base-300/50 border-b pb-4">
				<div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
					<LayoutList className="h-4 w-4" aria-hidden="true" />
				</div>
				<div>
					<h3 className="font-bold text-base tracking-tight">Stream Display</h3>
					<p className="text-base-content/60 text-xs">
						Choose which release details Stremio shows, and in what order
					</p>
				</div>
			</div>

			<div className="mt-5 space-y-5">
				<fieldset className="fieldset min-w-0">
					<legend className="fieldset-legend font-semibold text-xs">Format</legend>
					<div className="join flex-wrap">
						{options.map((opt) => (
							<button
								key={opt.id}
								type="button"
								className={`join-item btn btn-sm ${preset === opt.id ? "btn-primary" : "btn-outline"}`}
								disabled={isReadOnly}
								onClick={() => selectPreset(opt.id)}
								aria-pressed={preset === opt.id}
							>
								{opt.label}
							</button>
						))}
					</div>
				</fieldset>

				{preset === "custom" && (
					<div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
						<fieldset className="fieldset min-w-0">
							<legend className="fieldset-legend font-semibold text-xs">Name (left label)</legend>
							<textarea
								className="textarea w-full font-mono text-xs"
								rows={3}
								value={display.name_template ?? ""}
								disabled={isReadOnly}
								onChange={(e) => updateDisplay({ name_template: e.target.value })}
							/>
						</fieldset>
						<fieldset className="fieldset min-w-0">
							<legend className="fieldset-legend font-semibold text-xs">Description</legend>
							<textarea
								className="textarea w-full font-mono text-xs"
								rows={3}
								value={display.description_template ?? ""}
								disabled={isReadOnly}
								onChange={(e) => updateDisplay({ description_template: e.target.value })}
							/>
						</fieldset>
						<div className="space-y-2 text-base-content/60 text-xs lg:col-span-2">
							<p>
								Write <code>{"{token}"}</code> for a value and a new line (or <code>\n</code>) for a
								line break; an empty line adds spacing. Wrap a part in <code>{"< >"}</code> to drop
								it when a value inside is missing, e.g. <code>{"{title}< ({year})>"}</code>.
							</p>
							<div className="flex flex-wrap gap-1">
								{tokens.map((t) => (
									<span key={t} className="badge badge-ghost badge-sm font-mono">
										{`{${t}}`}
									</span>
								))}
							</div>
						</div>
					</div>
				)}

				<div className="space-y-2">
					<div className="font-semibold text-xs">Preview</div>
					{previewError && (
						<div className="alert alert-warning py-2 text-xs">
							<AlertTriangle className="h-4 w-4 shrink-0" aria-hidden="true" />
							<span>{previewError}</span>
						</div>
					)}
					<ul className="space-y-2">
						{previews.map((p) => (
							<li
								key={p.release}
								className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-3 rounded-xl border border-base-300 bg-base-100/70 p-3"
							>
								<div className="whitespace-pre-line break-words font-semibold text-xs">
									{p.library && (
										<span className="badge badge-secondary badge-xs mb-1 block w-fit font-normal">
											in your library
										</span>
									)}
									{p.name}
								</div>
								<div className="whitespace-pre-line break-words text-base-content/70 text-xs">
									{p.description}
								</div>
							</li>
						))}
					</ul>
					<p className="flex items-center gap-1 text-[11px] text-base-content/50">
						<Zap className="h-3 w-3" aria-hidden="true" />
						The first sample is a file already in your library (it keeps its own label on the
						Default format); the next is shown as already imported (cached).
					</p>
				</div>
			</div>
		</div>
	);
}
