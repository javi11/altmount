import { Eye, EyeOff, Trash2, Globe, Key, Tag, Activity } from "lucide-react";
import { useCallback, useState } from "react";
import type { ArrsInstanceConfig, ArrsType, SABnzbdCategory } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface ArrsInstanceCardProps {
	instance: ArrsInstanceConfig;
	type: ArrsType;
	index: number;
	isReadOnly: boolean;
	isApiKeyVisible: boolean;
	categories?: SABnzbdCategory[];
	onToggleApiKey: () => void;
	onRemove: () => void;
	onInstanceChange: (
		field: keyof ArrsInstanceConfig,
		value: ArrsInstanceConfig[keyof ArrsInstanceConfig],
	) => void;
}

export function ArrsInstanceCard({
	instance,
	type,
	index,
	isReadOnly,
	isApiKeyVisible,
	categories = [],
	onToggleApiKey,
	onRemove,
	onInstanceChange,
}: ArrsInstanceCardProps) {
	const instanceKey = `${type}-${index}`;
	const [testResult, setTestResult] = useState<{
		type: "success" | "error" | null;
		message: string;
	}>({ type: null, message: "" });
	const [isTestingConnection, setIsTestingConnection] = useState(false);

	const testConnection = useCallback(async () => {
		if (!instance.url || !instance.api_key) {
			setTestResult({ type: "error", message: "Required info missing" });
			return;
		}
		setIsTestingConnection(true);
		setTestResult({ type: null, message: "" });
		try {
			const response = await fetch("/api/arrs/instances/test", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ type: type, url: instance.url, api_key: instance.api_key }),
			});
			const data = await response.json();
			if (data.success) {
				setTestResult({ type: "success", message: data.message || "Link Active" });
			} else {
				setTestResult({ type: "error", message: data.error || "Link Failed" });
			}
		} catch (error) {
			setTestResult({ type: "error", message: "Network Error" });
		} finally {
			setIsTestingConnection(false);
			setTimeout(() => { setTestResult({ type: null, message: "" }); }, 5000);
		}
	}, [instance.url, instance.api_key, type]);

	const handleInstanceChange = useCallback(
		(field: keyof ArrsInstanceConfig, value: ArrsInstanceConfig[keyof ArrsInstanceConfig]) => {
			if (field === "url" || field === "api_key") setTestResult({ type: null, message: "" });
			onInstanceChange(field, value);
		},
		[onInstanceChange],
	);

	return (
		<div key={instanceKey} className="group relative rounded-2xl border border-base-300 bg-base-100/50 transition-all hover:shadow-md overflow-hidden">
			<div className={`absolute left-0 top-0 bottom-0 w-1.5 ${type === 'radarr' ? 'bg-primary' : 'bg-secondary'}`} />
			
			<div className="p-5 pl-7 space-y-6">
				{/* Header */}
				<div className="flex items-center justify-between gap-4">
					<div className="min-w-0">
						<div className="flex items-center gap-2">
							<span className="font-black text-[10px] uppercase tracking-tighter opacity-30">{type}</span>
							<h4 className="font-bold text-base tracking-tight break-all">{instance.name || 'Unnamed Instance'}</h4>
						</div>
					</div>
					<button
						type="button"
						className="btn btn-ghost btn-xs text-error opacity-0 group-hover:opacity-100 transition-opacity"
						onClick={onRemove}
						disabled={isReadOnly}
					>
						<Trash2 className="h-3.5 w-3.5" />
					</button>
				</div>

				<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend font-semibold flex items-center gap-2">
							<Globe className="h-3 w-3 opacity-40" /> URL
						</legend>
						<input
							type="url"
							className="input input-sm input-bordered w-full bg-base-100 font-mono text-xs"
							value={instance.url}
							onChange={(e) => handleInstanceChange("url", e.target.value)}
							placeholder="http://localhost:7878"
							disabled={isReadOnly}
						/>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend font-semibold flex items-center gap-2">
							<Key className="h-3 w-3 opacity-40" /> API Key
						</legend>
						<div className="join w-full shadow-sm">
							<input
								type={isApiKeyVisible ? "text" : "password"}
								className="input input-sm input-bordered join-item flex-1 bg-base-100 font-mono text-xs"
								value={instance.api_key}
								onChange={(e) => handleInstanceChange("api_key", e.target.value)}
								placeholder="Paste key..."
								disabled={isReadOnly}
							/>
							<button type="button" className="btn btn-sm btn-ghost border-base-300 join-item px-2" onClick={onToggleApiKey} disabled={isReadOnly}>
								{isApiKeyVisible ? <EyeOff className="h-3.5 w-3.5 opacity-50" /> : <Eye className="h-3.5 w-3.5 opacity-50" />}
							</button>
						</div>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend font-semibold flex items-center gap-2">
							<Tag className="h-3 w-3 opacity-40" /> Category
						</legend>
						<select
							className="select select-sm select-bordered w-full bg-base-100 text-xs font-bold"
							value={instance.category || ""}
							onChange={(e) => handleInstanceChange("category", e.target.value)}
							disabled={isReadOnly}
						>
							<option value="">(Automatic)</option>
							{categories.map((cat) => <option key={cat.name} value={cat.name}>{cat.name}</option>)}
						</select>
					</fieldset>

					<div className="flex flex-col justify-center pt-2">
						<label className="label cursor-pointer justify-start gap-3">
							<input
								type="checkbox"
								className="checkbox checkbox-xs checkbox-primary"
								checked={instance.enabled}
								onChange={(e) => handleInstanceChange("enabled", e.target.checked)}
								disabled={isReadOnly}
							/>
							<span className="label-text text-xs font-bold uppercase tracking-wider">Active</span>
						</label>
					</div>
				</div>

				{/* Quick Actions & Status */}
				<div className="flex items-center justify-between gap-4 pt-4 border-t border-base-300/50">
					<div className="flex-1">
						{testResult.type && (
							<div className={`flex items-center gap-2 font-black text-[10px] uppercase tracking-widest animate-in fade-in slide-in-from-left-2 ${testResult.type === 'success' ? 'text-success' : 'text-error'}`}>
								<Activity className="h-3 w-3" /> {testResult.message}
							</div>
						)}
					</div>
					<button
						type="button"
						className={`btn btn-xs shadow-sm px-4 ${
							isTestingConnection ? "btn-disabled" : testResult.type === "success" ? "btn-success" : testResult.type === "error" ? "btn-error" : "btn-outline border-base-300"
						}`}
						onClick={testConnection}
						disabled={isReadOnly || isTestingConnection || !instance.url || !instance.api_key}
					>
						{isTestingConnection ? <LoadingSpinner size="sm" /> : "Test Link"}
					</button>
				</div>
			</div>
		</div>
	);
}

export default ArrsInstanceCard;
