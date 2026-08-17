import { Crown, Edit2, Plus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import type { ScoringPreset, StreamScoringConfig, TrashCustomFormat } from "../../../types/config";
import { CustomFormatModal } from "./CustomFormatModal";
import { getDefaultFormatsForPreset } from "./scoringPresets";

interface TrashScoringCardProps {
	scoringConfig: StreamScoringConfig;
	onChange: (updated: StreamScoringConfig) => void;
	isReadOnly?: boolean;
}

const CATEGORY_COLORS: Record<string, string> = {
	source: "badge-info",
	hdr: "badge-secondary",
	audio: "badge-accent",
	release_group: "badge-warning",
	resolution: "badge-primary",
	custom: "badge-neutral",
};

export function TrashScoringCard({
	scoringConfig,
	onChange,
	isReadOnly = false,
}: TrashScoringCardProps) {
	const [activeCategory, setActiveCategory] = useState<string>("all");
	const [showModal, setShowModal] = useState(false);
	const [editingFormat, setEditingFormat] = useState<TrashCustomFormat | null>(null);

	const handlePresetChange = (newPreset: ScoringPreset) => {
		if (newPreset === scoringConfig.preset) return;

		if (newPreset === "custom") {
			onChange({ ...scoringConfig, preset: "custom" });
			return;
		}

		const defaultFormats = getDefaultFormatsForPreset(newPreset);
		// Keep user custom formats
		const customFormats = (scoringConfig.custom_formats || []).filter((f) => f.isCustom);
		onChange({
			...scoringConfig,
			preset: newPreset,
			custom_formats: [...defaultFormats, ...customFormats],
		});
	};

	const handleFormatScoreChange = (id: string, newScore: number) => {
		const updated = (scoringConfig.custom_formats || []).map((f) =>
			f.id === id ? { ...f, score: newScore } : f,
		);
		onChange({ ...scoringConfig, custom_formats: updated, preset: "custom" });
	};

	const handleFormatToggle = (id: string, enabled: boolean) => {
		const updated = (scoringConfig.custom_formats || []).map((f) =>
			f.id === id ? { ...f, enabled } : f,
		);
		onChange({ ...scoringConfig, custom_formats: updated, preset: "custom" });
	};

	const handleDeleteFormat = (id: string) => {
		const updated = (scoringConfig.custom_formats || []).filter((f) => f.id !== id);
		onChange({ ...scoringConfig, custom_formats: updated, preset: "custom" });
	};

	const handleSaveModalFormat = (format: TrashCustomFormat) => {
		const existing = (scoringConfig.custom_formats || []).some((f) => f.id === format.id);
		let updated: TrashCustomFormat[];
		if (existing) {
			updated = scoringConfig.custom_formats.map((f) => (f.id === format.id ? format : f));
		} else {
			updated = [...(scoringConfig.custom_formats || []), format];
		}
		onChange({ ...scoringConfig, custom_formats: updated, preset: "custom" });
	};

	const filteredFormats = useMemo(() => {
		const formats = scoringConfig.custom_formats || [];
		if (activeCategory === "all") return formats;
		if (activeCategory === "custom") return formats.filter((f) => f.isCustom);
		return formats.filter((f) => f.category === activeCategory);
	}, [scoringConfig.custom_formats, activeCategory]);

	return (
		<div className="min-w-0 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 shadow-sm">
			{/* Section Header */}
			<div className="flex flex-wrap items-center justify-between gap-4 border-base-300/50 border-b pb-4">
				<div className="flex items-center gap-2.5">
					<div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
						<Crown className="h-4 w-4 text-warning" />
					</div>
					<div>
						<h3 className="font-bold text-base tracking-tight">
							TRaSH Custom Format Scoring & Quality Profiles
						</h3>
						<p className="text-base-content/60 text-xs">
							Automated point ranking engine for 4K Remux, Dolby Vision, HDR10+, lossless audio, and
							releases
						</p>
					</div>
				</div>

				<button
					type="button"
					className="btn btn-outline btn-sm gap-1.5 shadow-sm"
					onClick={() => {
						setEditingFormat(null);
						setShowModal(true);
					}}
					disabled={isReadOnly}
				>
					<Plus className="h-4 w-4 text-primary" />
					<span>Add Custom Format</span>
				</button>
			</div>

			<div className="mt-5 space-y-6">
				{/* Preset Profile Dropdown */}
				<fieldset className="fieldset min-w-0">
					<legend className="fieldset-legend font-semibold text-xs">Scoring Preset Profile</legend>
					<div className="relative">
						<select
							className="select select-bordered w-full font-semibold text-xs"
							value={scoringConfig.preset || "trash_recommended"}
							disabled={isReadOnly}
							onChange={(e) => handlePresetChange(e.target.value as ScoringPreset)}
						>
							<option value="trash_recommended">
								🏆 TRaSH Recommended (High-Quality Remux & HDR Priority)
							</option>
							<option value="remux_enthusiast">
								💎 4K Remux Enthusiast (Disc ISO & Lossless Bitrate Focus)
							</option>
							<option value="compatibility">
								📱 Compatibility (Universal Direct Play / Low Bitrate WEB-DL)
							</option>
							<option value="custom">🛠️ Custom User-Defined Weights</option>
						</select>
					</div>
					<p className="label text-base-content/50 text-xs">
						Select a community-curated scoring baseline or fine-tune individual weights below.
					</p>
				</fieldset>

				{/* Category Tabs */}
				<div className="flex flex-wrap items-center gap-1.5 border-base-300 border-b pb-2">
					{[
						{ id: "all", label: "All Formats" },
						{ id: "source", label: "Source" },
						{ id: "hdr", label: "HDR" },
						{ id: "audio", label: "Audio" },
						{ id: "release_group", label: "Release Group" },
						{ id: "resolution", label: "Resolution" },
						{ id: "custom", label: "Custom Rules" },
					].map((tab) => (
						<button
							key={tab.id}
							type="button"
							className={`btn btn-xs rounded-lg whitespace-nowrap ${
								activeCategory === tab.id
									? "btn-primary shadow-sm"
									: "btn-ghost text-base-content/70 hover:bg-base-300"
							}`}
							onClick={() => setActiveCategory(tab.id)}
						>
							{tab.label}
						</button>
					))}
				</div>

				{/* Formats Table */}
				<div className="overflow-x-auto rounded-xl border border-base-300 bg-base-100/80">
					<table className="table table-sm">
						<thead>
							<tr className="border-base-300 bg-base-200/50 text-[11px] uppercase tracking-wider">
								<th className="w-10 text-center">Active</th>
								<th className="min-w-36">Custom Format Tag</th>
								<th className="w-32 whitespace-nowrap">Category</th>
								<th className="w-28 whitespace-nowrap text-center">Score</th>
								<th className="min-w-44">Weight Adjustment</th>
								<th className="w-16 text-right">Actions</th>
							</tr>
						</thead>
						<tbody>
							{filteredFormats.length === 0 ? (
								<tr>
									<td colSpan={6} className="py-6 text-center text-base-content/50 text-xs">
										No custom formats found in this category.
									</td>
								</tr>
							) : (
								filteredFormats.map((format) => {
									const isDiscard = format.score <= -1500;
									return (
										<tr
											key={format.id}
											className={`hover:bg-base-200/40 ${!format.enabled ? "opacity-40" : ""}`}
										>
											<td className="text-center">
												<input
													type="checkbox"
													className="checkbox checkbox-xs checkbox-primary"
													checked={format.enabled}
													disabled={isReadOnly}
													onChange={(e) => handleFormatToggle(format.id, e.target.checked)}
												/>
											</td>
											<td>
												<div className="font-semibold text-base-content/90 text-xs">
													{format.name}
												</div>
												<div className="max-w-xs truncate font-mono text-[10px] text-base-content/50">
													{format.pattern}
												</div>
											</td>
											<td className="whitespace-nowrap">
												<span
													className={`badge badge-xs uppercase font-medium whitespace-nowrap px-2 py-0.5 ${
														CATEGORY_COLORS[format.category] || "badge-ghost"
													}`}
												>
													{format.category.replace("_", " ")}
												</span>
											</td>
											<td className="whitespace-nowrap text-center">
												{isDiscard ? (
													<span className="badge badge-error badge-xs font-semibold whitespace-nowrap px-2 py-0.5">
														🚫 Discard
													</span>
												) : (
													<span
														className={`badge badge-xs font-bold font-mono whitespace-nowrap px-2 py-0.5 ${
															format.score > 0
																? "badge-success"
																: format.score < 0
																	? "badge-error"
																	: "badge-ghost"
														}`}
													>
														{format.score > 0 ? `+${format.score}` : format.score} pts
													</span>
												)}
											</td>
											<td>
												<div className="flex items-center gap-2">
													<input
														type="range"
														min="-2000"
														max="2000"
														step="25"
														value={format.score}
														disabled={isReadOnly || !format.enabled}
														className={`range range-xs ${
															format.score > 0
																? "range-success"
																: format.score < 0
																	? "range-error"
																	: "range-primary"
														}`}
														onChange={(e) =>
															handleFormatScoreChange(format.id, Number(e.target.value))
														}
													/>
												</div>
											</td>
											<td className="text-right">
												<div className="flex items-center justify-end gap-1">
													<button
														type="button"
														className="btn btn-ghost btn-xs text-base-content/60 hover:text-primary"
														onClick={() => {
															setEditingFormat(format);
															setShowModal(true);
														}}
														title="Edit rule"
														disabled={isReadOnly}
													>
														<Edit2 className="h-3.5 w-3.5" />
													</button>
													{format.isCustom && (
														<button
															type="button"
															className="btn btn-ghost btn-xs text-error hover:bg-error/10"
															onClick={() => handleDeleteFormat(format.id)}
															title="Delete custom rule"
															disabled={isReadOnly}
														>
															<Trash2 className="h-3.5 w-3.5" />
														</button>
													)}
												</div>
											</td>
										</tr>
									);
								})
							)}
						</tbody>
					</table>
				</div>
			</div>

			{/* Custom Format Builder Modal */}
			<CustomFormatModal
				isOpen={showModal}
				onClose={() => {
					setShowModal(false);
					setEditingFormat(null);
				}}
				onSave={handleSaveModalFormat}
				editingFormat={editingFormat}
			/>
		</div>
	);
}
