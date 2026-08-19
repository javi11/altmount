import { ArrowDown, ArrowUp, Globe, Languages, ShieldAlert, Trash2, X } from "lucide-react";
import { useState } from "react";
import type { StreamScoringConfig } from "../../../types/config";
import { LANGUAGE_OPTIONS } from "./scoringPresets";

interface FilterExclusionsCardProps {
	scoringConfig: StreamScoringConfig;
	onChange: (updated: StreamScoringConfig) => void;
	isReadOnly?: boolean;
}

export function FilterExclusionsCard({
	scoringConfig,
	onChange,
	isReadOnly = false,
}: FilterExclusionsCardProps) {
	const [keywordInput, setKeywordInput] = useState("");
	const [selectedLangToAdd, setSelectedLangToAdd] = useState("");

	const excludeKeywords = scoringConfig.exclude_keywords || [];
	const preferredLanguages = scoringConfig.preferred_languages || [];
	const requireLanguage = scoringConfig.require_preferred_language ?? false;

	// Blacklist keyword management
	const handleAddKeyword = () => {
		const kw = keywordInput.trim();
		if (!kw) return;
		if (!excludeKeywords.includes(kw)) {
			onChange({
				...scoringConfig,
				exclude_keywords: [...excludeKeywords, kw],
			});
		}
		setKeywordInput("");
	};

	const handleRemoveKeyword = (kwToRemove: string) => {
		onChange({
			...scoringConfig,
			exclude_keywords: excludeKeywords.filter((k) => k !== kwToRemove),
		});
	};

	// Language priority matrix management
	const handleMoveLanguage = (index: number, direction: "up" | "down") => {
		const newIndex = direction === "up" ? index - 1 : index + 1;
		if (newIndex < 0 || newIndex >= preferredLanguages.length) return;

		const updated = [...preferredLanguages];
		const temp = updated[index];
		updated[index] = updated[newIndex];
		updated[newIndex] = temp;

		onChange({
			...scoringConfig,
			preferred_languages: updated,
		});
	};

	const handleAddLanguage = (langName: string) => {
		if (!langName || preferredLanguages.includes(langName)) return;
		onChange({
			...scoringConfig,
			preferred_languages: [...preferredLanguages, langName],
		});
		setSelectedLangToAdd("");
	};

	const handleRemoveLanguage = (langName: string) => {
		onChange({
			...scoringConfig,
			preferred_languages: preferredLanguages.filter((l) => l !== langName),
		});
	};

	return (
		<div className="min-w-0 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 shadow-sm">
			{/* Section Header */}
			<div className="flex items-center gap-2.5 border-base-300/50 border-b pb-4">
				<div className="flex h-8 w-8 items-center justify-center rounded-lg bg-error/10 text-error">
					<ShieldAlert className="h-4 w-4" />
				</div>
				<div>
					<h3 className="font-bold text-base tracking-tight">
						Exclude Keywords & Audio Language Matrix
					</h3>
					<p className="text-base-content/60 text-xs">
						Blacklist unwanted releases and rank stream results by audio language preference
					</p>
				</div>
			</div>

			<div className="mt-5 space-y-6">
				{/* Blacklist Keywords Tag Chip Input */}
				<div className="space-y-2">
					<div className="font-semibold text-base-content/70 text-xs">
						Negative Blacklist Keywords (Excluded automatically)
					</div>
					<div className="flex min-h-12 min-w-0 flex-wrap items-center gap-2 rounded-xl border border-base-300 bg-base-100/90 p-2.5 shadow-inner">
						{excludeKeywords.map((keyword) => (
							<span
								key={keyword}
								className="badge badge-error gap-1.5 py-3 font-medium font-mono text-xs shadow-sm"
							>
								<span>{keyword}</span>
								{!isReadOnly && (
									<button
										type="button"
										onClick={() => handleRemoveKeyword(keyword)}
										className="hover:opacity-75"
										aria-label={`Remove ${keyword}`}
									>
										<X className="h-3 w-3" />
									</button>
								)}
							</span>
						))}

						{!isReadOnly && (
							<div className="flex min-w-40 flex-1 items-center">
								<input
									type="text"
									className="input input-ghost input-xs w-full text-xs focus:outline-none"
									placeholder="Type keyword / regex and press Enter..."
									value={keywordInput}
									onChange={(e) => setKeywordInput(e.target.value)}
									onKeyDown={(e) => {
										if (e.key === "Enter" || e.key === ",") {
											e.preventDefault();
											handleAddKeyword();
										}
									}}
									onBlur={handleAddKeyword}
								/>
							</div>
						)}
					</div>
					<p className="text-[11px] text-base-content/50">
						Releases containing any of these keywords will be discarded from Stremio stream results.
					</p>
				</div>

				{/* Preferred Audio Languages Matrix */}
				<div className="space-y-3">
					<div className="flex flex-wrap items-center justify-between gap-2">
						<div className="flex items-center gap-1.5 font-semibold text-base-content/70 text-xs">
							<Languages className="h-4 w-4 text-primary" />
							Preferred Audio Languages (Ranked Priority)
						</div>

						{!isReadOnly && (
							<div className="flex items-center gap-2">
								<select
									className="select select-bordered select-xs"
									value={selectedLangToAdd}
									onChange={(e) => handleAddLanguage(e.target.value)}
								>
									<option value="">+ Add Language...</option>
									{LANGUAGE_OPTIONS.filter((opt) => !preferredLanguages.includes(opt.name)).map(
										(opt) => (
											<option key={opt.id} value={opt.name}>
												{opt.flag} {opt.name}
											</option>
										),
									)}
								</select>
							</div>
						)}
					</div>

					<div className="space-y-2 rounded-xl border border-base-300 bg-base-100/90 p-3 shadow-inner">
						{preferredLanguages.length === 0 ? (
							<div className="py-4 text-center text-base-content/50 text-xs">
								No language preferences defined. Streams will be ordered purely by quality score.
							</div>
						) : (
							preferredLanguages.map((lang, index) => {
								const langInfo = LANGUAGE_OPTIONS.find(
									(l) => l.name.toLowerCase() === lang.toLowerCase(),
								);
								const flag = langInfo ? langInfo.flag : "🌐";
								return (
									<div
										key={lang}
										className="flex items-center justify-between rounded-lg border border-base-200 bg-base-200/50 px-3 py-2 text-xs"
									>
										<div className="flex items-center gap-3">
											<span className="font-bold font-mono text-base-content/40 text-xs">
												{index + 1}.
											</span>
											<span className="text-base">{flag}</span>
											<span className="font-semibold text-base-content/90">
												{lang}
												{index === 0 && (
													<span className="badge badge-primary badge-xs ml-2">Primary</span>
												)}
												{index === 1 && (
													<span className="badge badge-ghost badge-xs ml-2">Secondary</span>
												)}
											</span>
										</div>

										{!isReadOnly && (
											<div className="flex items-center gap-1">
												<button
													type="button"
													className="btn btn-ghost btn-xs h-6 w-6 p-0"
													disabled={index === 0}
													onClick={() => handleMoveLanguage(index, "up")}
													title="Move Up"
												>
													<ArrowUp className="h-3 w-3" />
												</button>
												<button
													type="button"
													className="btn btn-ghost btn-xs h-6 w-6 p-0"
													disabled={index === preferredLanguages.length - 1}
													onClick={() => handleMoveLanguage(index, "down")}
													title="Move Down"
												>
													<ArrowDown className="h-3 w-3" />
												</button>
												<button
													type="button"
													className="btn btn-ghost btn-xs h-6 w-6 p-0 text-error hover:bg-error/10"
													onClick={() => handleRemoveLanguage(lang)}
													title="Remove Language"
												>
													<Trash2 className="h-3 w-3" />
												</button>
											</div>
										)}
									</div>
								);
							})
						)}
					</div>
				</div>

				{/* Require Preferred Language Toggle */}
				<div className="flex items-center justify-between rounded-xl border border-base-300 bg-base-100/70 p-4">
					<div className="min-w-0 flex-1">
						<div className="flex items-center gap-2">
							<Globe className="h-4 w-4 text-primary" />
							<span className="font-semibold text-sm">
								Discard releases missing all preferred audio tracks
							</span>
						</div>
						<p className="mt-1 text-base-content/60 text-xs">
							When enabled, any release not containing at least one preferred language keyword will
							be dropped from stream results.
						</p>
					</div>
					<input
						type="checkbox"
						className="toggle toggle-primary shrink-0"
						checked={requireLanguage}
						disabled={isReadOnly}
						onChange={(e) =>
							onChange({
								...scoringConfig,
								require_preferred_language: e.target.checked,
							})
						}
					/>
				</div>
			</div>
		</div>
	);
}
