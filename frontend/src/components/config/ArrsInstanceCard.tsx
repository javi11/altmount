import { Eye, EyeOff, Trash2 } from "lucide-react";
import { useCallback, useState } from "react";
import type { ArrsInstanceConfig, ArrsType } from "../../types/config";

interface ArrsInstanceCardProps {
	instance: ArrsInstanceConfig;
	type: ArrsType;
	index: number;
	isReadOnly: boolean;
	isApiKeyVisible: boolean;
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
			<div className="card-body p-4">
				<div className="mb-4 flex items-center justify-between">
					<div className="flex items-center space-x-3">
						<h4 className="font-semibold capitalize">{type} Instance</h4>
					</div>
					<div className="flex items-center space-x-2">
						<button
							type="button"
							className="btn btn-sm btn-error btn-outline"
							onClick={onRemove}
							disabled={isReadOnly}
						>
							<Trash2 className="h-4 w-4" />
							Remove
						</button>
					</div>
				</div>

				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Instance Name</legend>
						<input
							type="text"
							className="input"
							value={instance.name}
							onChange={(e) => handleInstanceChange("name", e.target.value)}
							placeholder="My Radarr/Sonarr"
							disabled={isReadOnly}
						/>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">URL</legend>
						<input
							type="url"
							className="input"
							value={instance.url}
							onChange={(e) => handleInstanceChange("url", e.target.value)}
							placeholder="http://localhost:7878"
							disabled={isReadOnly}
						/>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">API Key</legend>
						<div className="flex">
							<input
								type={isApiKeyVisible ? "text" : "password"}
								className="input flex-1"
								value={instance.api_key}
								onChange={(e) => handleInstanceChange("api_key", e.target.value)}
								placeholder="API key from settings"
								disabled={isReadOnly}
							/>
							<button
								type="button"
								className="btn btn-outline ml-2"
								onClick={onToggleApiKey}
								disabled={isReadOnly}
							>
								{isApiKeyVisible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
							</button>
							<button
								type="button"
								className={`btn ml-2 ${
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
								aria-label="Test connection"
							>
								{isTestingConnection ? (
									<div className="loading loading-spinner h-4 w-4" />
								) : (
									"Test"
								)}
							</button>
						</div>
						{testResult.type && (
							<div
								className={`mt-2 text-sm ${
									testResult.type === "success" ? "text-success" : "text-error"
								}`}
							>
								{testResult.message}
							</div>
						)}
					</fieldset>
				</div>

				<div className="mt-4">
					<label className="label cursor-pointer">
						<span className="label-text">Enable this instance</span>
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
