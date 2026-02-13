import { Eye, EyeOff, Trash2 } from "lucide-react";
import { useCallback, useState } from "react";
import type { ArrsInstanceConfig, ArrsType, SABnzbdCategory } from "../../types/config";

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
			setTestResult({
				type: "error",
				message: "URL and API key are required",
			});
			return;
		}

		setIsTestingConnection(true);
		setTestResult({ type: null, message: "" });

		try {
			const response = await fetch("/api/arrs/instances/test", {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
				},
				body: JSON.stringify({
					type: type,
					url: instance.url,
					api_key: instance.api_key,
				}),
			});

			const data = await response.json();

			if (data.success) {
				setTestResult({
					type: "success",
					message: data.message || "Connection successful",
				});
			} else {
				setTestResult({
					type: "error",
					message: data.error || "Connection failed",
				});
			}
		} catch (error) {
			setTestResult({
				type: "error",
				message: error instanceof Error ? error.message : "Connection failed",
			});
		} finally {
			setIsTestingConnection(false);
			// Clear the result after 5 seconds
			setTimeout(() => {
				setTestResult({ type: null, message: "" });
			}, 5000);
		}
	}, [instance.url, instance.api_key, type]);

	// Clear test result when connection details change
	const handleInstanceChange = useCallback(
		(field: keyof ArrsInstanceConfig, value: ArrsInstanceConfig[keyof ArrsInstanceConfig]) => {
			if (field === "url" || field === "api_key") {
				setTestResult({ type: null, message: "" });
			}
			onInstanceChange(field, value);
		},
		[onInstanceChange],
	);

	return (
		<div key={instanceKey} className="card bg-base-200">
			<div className="card-body p-3 sm:p-4">
				<div className="mb-4 flex flex-wrap items-center justify-between gap-2">
					<div className="flex items-center space-x-3">
						<h4 className="font-semibold text-sm capitalize sm:text-base">{type} Instance</h4>
					</div>
					<div className="flex items-center">
						<button
							type="button"
							className="btn btn-xs sm:btn-sm btn-error btn-outline w-full sm:w-auto"
							onClick={onRemove}
							disabled={isReadOnly}
						>
							<Trash2 className="h-3 w-3 sm:h-4 sm:w-4" />
							Remove
						</button>
					</div>
				</div>

				<div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Name</legend>
						<input
							type="text"
							className="input w-full"
							value={instance.name}
							onChange={(e) => handleInstanceChange("name", e.target.value)}
							placeholder="My Instance"
							disabled={isReadOnly}
						/>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">URL</legend>
						<input
							type="url"
							className="input w-full"
							value={instance.url}
							onChange={(e) => handleInstanceChange("url", e.target.value)}
							placeholder="http://localhost:7878"
							disabled={isReadOnly}
						/>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Category</legend>
						<select
							className="select w-full"
							value={instance.category || ""}
							onChange={(e) => handleInstanceChange("category", e.target.value)}
							disabled={isReadOnly}
						>
							<option value="">None</option>
							{categories.map((cat) => (
								<option key={cat.name} value={cat.name}>
									{cat.name}
								</option>
							))}
						</select>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">API Key</legend>
						<div className="flex gap-1 sm:gap-2">
							<input
								type={isApiKeyVisible ? "text" : "password"}
								className="input min-w-0 flex-1"
								value={instance.api_key}
								onChange={(e) => handleInstanceChange("api_key", e.target.value)}
								placeholder="API key"
								disabled={isReadOnly}
							/>
							<button
								type="button"
								className="btn btn-outline btn-sm sm:btn-md shrink-0"
								onClick={onToggleApiKey}
								disabled={isReadOnly}
							>
								{isApiKeyVisible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
							</button>
							<button
								type="button"
								className={`btn btn-sm sm:btn-md shrink-0 ${
									isTestingConnection
										? "btn-disabled loading"
										: testResult.type === "success"
											? "btn-success"
											: testResult.type === "error"
												? "btn-error"
												: "btn-outline"
								}`}
								onClick={testConnection}
								disabled={isReadOnly || isTestingConnection || !instance.url || !instance.api_key}
							>
								{isTestingConnection ? <div className="loading loading-spinner h-4 w-4" /> : "Test"}
							</button>
						</div>
						{testResult.type && (
							<div
								className={`mt-2 break-words font-medium text-xs ${
									testResult.type === "success" ? "text-success" : "text-error"
								}`}
							>
								{testResult.message}
							</div>
						)}
					</fieldset>
				</div>

				<div className="mt-4">
					<label className="label cursor-pointer justify-start gap-4">
						<span className="label-text">Enable instance</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={instance.enabled}
							onChange={(e) => handleInstanceChange("enabled", e.target.checked)}
							disabled={isReadOnly}
						/>
					</label>
				</div>
			</div>
		</div>
	);
}

export default ArrsInstanceCard;
