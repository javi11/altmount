import { Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, NzblnkConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface NzblnkConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: NzblnkConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function NzblnkConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: NzblnkConfigSectionProps) {
	const [formData, setFormData] = useState<NzblnkConfig>(config.nzblnk ?? {});
	const [hasChanges, setHasChanges] = useState(false);

	useEffect(() => {
		setFormData(config.nzblnk ?? {});
		setHasChanges(false);
	}, [config.nzblnk]);

	const handleInputChange = (field: keyof NzblnkConfig, value: string) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.nzblnk ?? {}));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("nzblnk", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="min-w-0 space-y-10">
			<div className="min-w-0">
				<h3 className="font-bold text-base-content text-lg tracking-tight">NZBLNK Resolver</h3>
				<p className="break-words text-base-content/50 text-sm">
					Configure how nzblnk:// links are resolved via public NZB indexers.
				</p>
			</div>

			<div className="min-w-0 space-y-6 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
						HTTP Headers
					</h4>
					<div className="h-px flex-1 bg-base-300/50" />
				</div>

				<fieldset className="fieldset min-w-0">
					<legend className="fieldset-legend font-semibold">Indexer User-Agent</legend>
					<input
						type="text"
						className="input input-bordered w-full min-w-0 max-w-full bg-base-100 font-mono text-sm"
						value={formData.user_agent ?? ""}
						readOnly={isReadOnly}
						placeholder="Mozilla/5.0 ... (leave empty for default)"
						onChange={(e) => handleInputChange("user_agent", e.target.value)}
					/>
					<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/70 text-xs">
						HTTP User-Agent sent when searching and downloading from public NZB indexers (e.g.
						nzbking.com, nzbindex.com). Leave empty to use the built-in default.
					</p>
				</fieldset>
			</div>

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
