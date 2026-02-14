import { Info, Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, StreamingConfig } from "../../types/config";

interface StreamingConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: StreamingConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function StreamingConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: StreamingConfigSectionProps) {
	const [formData, setFormData] = useState<StreamingConfig>(config.streaming);
	const [hasChanges, setHasChanges] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.streaming);
		setHasChanges(false);
	}, [config.streaming]);

	const handleInputChange = (field: keyof StreamingConfig, value: number) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.streaming));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("streaming", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-10">
			<div>
				<h3 className="text-lg font-bold text-base-content">Playback Tuning</h3>
				<p className="text-sm text-base-content/50">Optimize how AltMount streams media to your players.</p>
			</div>

			<div className="space-y-8">
				{/* Prefetch Slider */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6">
					<div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
						<div className="min-w-0">
							<h4 className="font-bold text-sm text-base-content">Segment Prefetch</h4>
							<p className="text-[11px] text-base-content/50 mt-1 break-words leading-relaxed">
								Number of Usenet articles to download ahead of current playback position.
							</p>
						</div>
						<div className="flex items-center gap-3 shrink-0">
							<span className="font-mono font-black text-xl text-primary">{formData.max_prefetch}</span>
							<span className="text-[10px] font-bold uppercase opacity-40">segments</span>
						</div>
					</div>

					<div className="space-y-4">
						<input
							type="range"
							min="1"
							max="50"
							value={formData.max_prefetch}
							className="range range-primary range-sm"
							step="1"
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("max_prefetch", Number.parseInt(e.target.value, 10))}
						/>
						<div className="flex justify-between px-2 text-[10px] font-black opacity-30">
							<span>1</span>
							<span>10</span>
							<span>20</span>
							<span>30</span>
							<span>40</span>
							<span>50</span>
						</div>
					</div>
				</div>

				{/* Guidance */}
				<div className="alert rounded-2xl border border-info/20 bg-info/5 p-4 shadow-sm items-start">
					<Info className="h-5 w-5 text-info shrink-0 mt-0.5" />
					<div className="min-w-0 flex-1">
						<div className="font-bold text-xs uppercase tracking-wider text-info">Performance Note</div>
						<div className="text-[11px] leading-relaxed mt-1 break-words opacity-80">
							Higher values improve stability on slow connections but increase initial memory usage. 
							Default (3) is recommended for most 4K streaming scenarios.
						</div>
					</div>
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end pt-4 border-t border-base-200">
					<button
						type="button"
						className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${!hasChanges && 'btn-ghost border-base-300'}`}
						onClick={handleSave}
						disabled={!hasChanges || isUpdating}
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
		</div>
	);
}
