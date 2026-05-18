import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

interface KeyValueEditorProps {
	value: Record<string, string>;
	onChange: (value: Record<string, string>) => void;
	keyPlaceholder?: string;
	valuePlaceholder?: string;
	disabled?: boolean;
}

export function KeyValueEditor({
	value,
	onChange,
	keyPlaceholder = "Key",
	valuePlaceholder = "Value",
	disabled = false,
}: KeyValueEditorProps) {
	const [newKey, setNewKey] = useState("");
	const [newValue, setNewValue] = useState("");

	const handleAdd = () => {
		if (!newKey.trim()) return;
		const updated = { ...value, [newKey.trim()]: newValue.trim() };
		onChange(updated);
		setNewKey("");
		setNewValue("");
	};

	const handleRemove = (key: string) => {
		const updated = { ...value };
		delete updated[key];
		onChange(updated);
	};

	const handleValueChange = (key: string, val: string) => {
		const updated = { ...value, [key]: val };
		onChange(updated);
	};

	return (
		<div className="space-y-4">
			<div className="space-y-2">
				{Object.entries(value).map(([key, val]) => (
					<div key={key} className="flex items-center gap-2">
						<input
							type="text"
							className="input input-sm input-bordered flex-1 font-mono text-xs"
							value={key}
							readOnly
							disabled={disabled}
						/>
						<input
							type="text"
							className="input input-sm input-bordered flex-1 font-mono text-xs"
							value={val}
							placeholder={valuePlaceholder}
							disabled={disabled}
							onChange={(e) => handleValueChange(key, e.target.value)}
						/>
						{!disabled && (
							<button
								type="button"
								className="btn btn-square btn-ghost btn-sm text-error"
								onClick={() => handleRemove(key)}
							>
								<Trash2 className="h-4 w-4" />
							</button>
						)}
					</div>
				))}
			</div>

			{!disabled && (
				<div className="flex items-center gap-2 rounded-lg border-2 border-base-300 border-dashed p-2">
					<input
						type="text"
						className="input input-sm input-bordered flex-1 font-mono text-xs"
						placeholder={keyPlaceholder}
						value={newKey}
						onChange={(e) => setNewKey(e.target.value)}
					/>
					<input
						type="text"
						className="input input-sm input-bordered flex-1 font-mono text-xs"
						placeholder={valuePlaceholder}
						value={newValue}
						onChange={(e) => setNewValue(e.target.value)}
					/>
					<button
						type="button"
						className="btn btn-square btn-primary btn-sm"
						onClick={handleAdd}
						disabled={!newKey.trim()}
					>
						<Plus className="h-4 w-4" />
					</button>
				</div>
			)}
		</div>
	);
}
