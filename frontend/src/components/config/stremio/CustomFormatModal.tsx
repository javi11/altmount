import { Sparkles, X } from "lucide-react";
import { useEffect, useState } from "react";
import type { TrashCustomFormat } from "../../../types/config";

interface CustomFormatModalProps {
	isOpen: boolean;
	onClose: () => void;
	onSave: (format: TrashCustomFormat) => void;
	editingFormat?: TrashCustomFormat | null;
}

const DEFAULT_NEW_FORMAT: Omit<TrashCustomFormat, "id"> = {
	name: "",
	category: "custom",
	pattern: "",
	patternType: "regex",
	score: 250,
	enabled: true,
	isCustom: true,
	invert: false,
};

export function CustomFormatModal({
	isOpen,
	onClose,
	onSave,
	editingFormat,
}: CustomFormatModalProps) {
	const [formData, setFormData] = useState<Omit<TrashCustomFormat, "id">>(DEFAULT_NEW_FORMAT);
	const [patternError, setPatternError] = useState<string | null>(null);

	useEffect(() => {
		if (editingFormat) {
			setFormData({
				name: editingFormat.name,
				category: editingFormat.category,
				pattern: editingFormat.pattern,
				patternType: editingFormat.patternType,
				score: editingFormat.score,
				enabled: editingFormat.enabled,
				isCustom: true,
				invert: editingFormat.invert ?? false,
			});
		} else {
			setFormData(DEFAULT_NEW_FORMAT);
		}
		setPatternError(null);
	}, [editingFormat]);

	if (!isOpen) return null;

	const validatePattern = (pattern: string, type: "regex" | "token") => {
		if (type === "regex" && pattern.trim()) {
			try {
				new RegExp(pattern);
				setPatternError(null);
			} catch (e) {
				setPatternError(e instanceof Error ? e.message : "Invalid Regular Expression");
			}
		} else {
			setPatternError(null);
		}
	};

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();
		if (!formData.name.trim() || !formData.pattern.trim() || patternError) return;

		const formatId = editingFormat
			? editingFormat.id
			: `custom_${Date.now()}_${Math.random().toString(36).substring(2, 6)}`;
		onSave({
			id: formatId,
			...formData,
			name: formData.name.trim(),
			pattern: formData.pattern.trim(),
		});
		onClose();
	};

	return (
		<div className="modal modal-open z-50">
			<div className="modal-box relative max-w-lg border border-base-300 bg-base-100 p-6 shadow-2xl">
				<button
					type="button"
					className="btn btn-circle btn-ghost btn-sm absolute top-4 right-4"
					onClick={onClose}
					aria-label="Close"
				>
					<X className="h-4 w-4" />
				</button>

				<div className="flex items-center gap-3">
					<div className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 text-primary">
						<Sparkles className="h-5 w-5" />
					</div>
					<div>
						<h3 className="font-bold text-lg">
							{editingFormat ? "Edit Custom Format" : "Add TRaSH Custom Format"}
						</h3>
						<p className="text-base-content/60 text-xs">
							Define custom regex or keyword pattern rules for stream ranking
						</p>
					</div>
				</div>

				<form onSubmit={handleSubmit} className="mt-6 space-y-4">
					{/* Rule Name */}
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold text-xs">Rule Name</legend>
						<input
							type="text"
							className="input input-bordered w-full text-xs"
							placeholder="e.g. 4K Disc Remux (Untouched)"
							value={formData.name}
							required
							onChange={(e) => setFormData((p) => ({ ...p, name: e.target.value }))}
						/>
					</fieldset>

					{/* Category & Pattern Type */}
					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
						<fieldset className="fieldset min-w-0">
							<legend className="fieldset-legend font-semibold text-xs">Category</legend>
							<select
								className="select select-bordered w-full text-xs"
								value={formData.category}
								onChange={(e) =>
									setFormData((p) => ({
										...p,
										category: e.target.value as TrashCustomFormat["category"],
									}))
								}
							>
								<option value="source">Source (Disc/Remux/WEB)</option>
								<option value="hdr">HDR (DV / HDR10+)</option>
								<option value="audio">Audio (TrueHD / Atmos / DTS)</option>
								<option value="release_group">Release Group</option>
								<option value="resolution">Resolution</option>
								<option value="custom">Custom Tag</option>
							</select>
						</fieldset>

						<fieldset className="fieldset min-w-0">
							<legend className="fieldset-legend font-semibold text-xs">Pattern Type</legend>
							<select
								className="select select-bordered w-full text-xs"
								value={formData.patternType}
								onChange={(e) => {
									const type = e.target.value as "regex" | "token";
									setFormData((p) => ({ ...p, patternType: type }));
									validatePattern(formData.pattern, type);
								}}
							>
								<option value="regex">Regular Expression (Regex)</option>
								<option value="token">Exact Substring / Token</option>
							</select>
						</fieldset>
					</div>

					{/* Pattern String */}
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold text-xs">Match Pattern</legend>
						<input
							type="text"
							className={`input input-bordered w-full font-mono text-xs ${
								patternError ? "input-error" : ""
							}`}
							placeholder={
								formData.patternType === "regex"
									? "\\b(remux|bdremux)\\b.*\\b(2160p|4k)\\b"
									: "REMUX"
							}
							value={formData.pattern}
							required
							onChange={(e) => {
								const val = e.target.value;
								setFormData((p) => ({ ...p, pattern: val }));
								validatePattern(val, formData.patternType);
							}}
						/>
						{patternError ? (
							<p className="label text-[11px] text-error">{patternError}</p>
						) : (
							<p className="label text-[11px] text-base-content/50">
								{formData.patternType === "regex"
									? "Case-insensitive ECMAScript regex syntax."
									: "Exact case-insensitive token matched against release title."}
							</p>
						)}
					</fieldset>

					{/* Score Weight Slider & Input */}
					<fieldset className="fieldset min-w-0">
						<div className="flex items-center justify-between">
							<legend className="fieldset-legend font-semibold text-xs">Score Points</legend>
							<span
								className={`badge font-bold font-mono text-xs whitespace-nowrap px-2.5 py-0.5 ${
									formData.score > 0
										? "badge-success"
										: formData.score < 0
											? "badge-error"
											: "badge-ghost"
								}`}
							>
								{formData.score > 0 ? `+${formData.score}` : formData.score} pts
							</span>
						</div>

						<div className="flex items-center gap-4">
							<input
								type="range"
								min="-2000"
								max="2000"
								step="25"
								value={formData.score}
								className={`range range-xs flex-1 ${
									formData.score > 0
										? "range-success"
										: formData.score < 0
											? "range-error"
											: "range-primary"
								}`}
								onChange={(e) => setFormData((p) => ({ ...p, score: Number(e.target.value) }))}
							/>
							<input
								type="number"
								min="-2000"
								max="2000"
								value={formData.score}
								className="input input-bordered input-sm w-24 text-right font-mono text-xs"
								onChange={(e) =>
									setFormData((p) => ({
										...p,
										score: Math.min(2000, Math.max(-2000, Number(e.target.value) || 0)),
									}))
								}
							/>
						</div>
						<p className="label text-[11px] text-base-content/50">
							Positive scores promote candidate releases. Scores ≤ -1500 discard the stream.
						</p>
					</fieldset>

					{/* Invert Match Toggle */}
					<div className="flex items-center justify-between rounded-xl border border-base-300 bg-base-200/50 p-3">
						<div>
							<span className="font-semibold text-xs">Invert Match (Negate)</span>
							<p className="text-[11px] text-base-content/60">
								Apply points when the title DOES NOT match this pattern
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary toggle-sm"
							checked={formData.invert}
							onChange={(e) => setFormData((p) => ({ ...p, invert: e.target.checked }))}
						/>
					</div>

					{/* Modal Actions */}
					<div className="modal-action gap-2 pt-2">
						<button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>
							Cancel
						</button>
						<button
							type="submit"
							className="btn btn-primary btn-sm px-6 shadow-md shadow-primary/20"
							disabled={!formData.name.trim() || !formData.pattern.trim() || !!patternError}
						>
							{editingFormat ? "Save Changes" : "Add Format Rule"}
						</button>
					</div>
				</form>
			</div>
			<button type="button" className="modal-backdrop" onClick={onClose} aria-label="Close modal">
				Close
			</button>
		</div>
	);
}
