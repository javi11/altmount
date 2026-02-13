import { AlertTriangle, Check, Loader, Wifi } from "lucide-react";
import { useEffect, useState } from "react";
import { useToast } from "../../contexts/ToastContext";
import { useProviders } from "../../hooks/useProviders";
import type { ProviderConfig, ProviderFormData } from "../../types/config";

interface ProviderModalProps {
	mode: "create" | "edit";
	provider?: ProviderConfig | null;
	onSuccess: () => void;
	onCancel: () => void;
}

const defaultFormData: ProviderFormData = {
	host: "",
	port: 119,
	username: "",
	password: "",
	max_connections: 10,
	inflight_requests: 10,
	tls: false,
	insecure_tls: false,
	proxy_url: "",
	enabled: true,
	is_backup_provider: false,
};

export function ProviderModal({ mode, provider, onSuccess, onCancel }: ProviderModalProps) {
	const [formData, setFormData] = useState<ProviderFormData>(defaultFormData);
	const [isTestingConnection, setIsTestingConnection] = useState(false);
	const [connectionTestResult, setConnectionTestResult] = useState<{
		success: boolean;
		message?: string;
		rttMs?: number;
	} | null>(null);
	const [canSave, setCanSave] = useState(false);

	const { testProvider, createProvider, updateProvider } = useProviders();
	const { showToast } = useToast();

	// Initialize form data when provider changes
	useEffect(() => {
		if (mode === "edit" && provider) {
			setFormData({
				host: provider.host,
				port: provider.port,
				username: provider.username,
				password: "", // Always start with empty password for security
				max_connections: provider.max_connections,
				inflight_requests: provider.inflight_requests || 10,
				tls: provider.tls,
				insecure_tls: provider.insecure_tls,
				proxy_url: provider.proxy_url || "",
				enabled: provider.enabled,
				is_backup_provider: provider.is_backup_provider,
			});
			// For edit mode, allow saving without testing if only non-connection fields change
			setCanSave(true);
		} else {
			setFormData(defaultFormData);
			setCanSave(false);
		}
		setConnectionTestResult(null);
	}, [mode, provider]);

	const handleInputChange = (field: keyof ProviderFormData, value: string | number | boolean) => {
		setFormData((prev) => ({ ...prev, [field]: value }));

		// Reset connection test if connection-related fields change
		if (
			["host", "port", "username", "password", "tls", "insecure_tls", "proxy_url"].includes(field)
		) {
			setConnectionTestResult(null);
			setCanSave(false);
		}
	};

	const handleTestConnection = async () => {
		if (!formData.host || !formData.username || !formData.password) {
			showToast({
				type: "warning",
				title: "Missing Required Fields",
				message: "Please fill in all required fields before testing connection",
			});
			return;
		}

		setIsTestingConnection(true);
		setConnectionTestResult(null);

		try {
			const result = await testProvider.mutateAsync({
				host: formData.host,
				port: formData.port,
				username: formData.username,
				password: formData.password,
				tls: formData.tls,
				insecure_tls: formData.insecure_tls,
				proxy_url: formData.proxy_url || undefined,
			});

			setConnectionTestResult({
				success: result.success,
				message: result.error_message,
				rttMs: result.rtt_ms,
			});

			setCanSave(result.success);
		} catch (error) {
			setConnectionTestResult({
				success: false,
				message: error instanceof Error ? error.message : "Connection test failed",
			});
			setCanSave(false);
		} finally {
			setIsTestingConnection(false);
		}
	};

	const handleSave = async () => {
		if (!canSave) {
			showToast({
				type: "warning",
				title: "Connection Test Required",
				message: "Please test the connection successfully before saving",
			});
			return;
		}

		try {
			if (mode === "create") {
				await createProvider.mutateAsync({
					...formData,
					proxy_url: formData.proxy_url || undefined,
				});
			} else if (mode === "edit" && provider) {
				// Only send changed fields for update
				const updateData: Partial<ProviderFormData> = {};

				if (formData.host !== provider.host) updateData.host = formData.host;
				if (formData.port !== provider.port) updateData.port = formData.port;
				if (formData.username !== provider.username) updateData.username = formData.username;
				if (formData.password) updateData.password = formData.password; // Only include if not empty
				if (formData.max_connections !== provider.max_connections)
					updateData.max_connections = formData.max_connections;
				if (formData.inflight_requests !== provider.inflight_requests)
					updateData.inflight_requests = formData.inflight_requests;
				if (formData.tls !== provider.tls) updateData.tls = formData.tls;
				if (formData.insecure_tls !== provider.insecure_tls)
					updateData.insecure_tls = formData.insecure_tls;
				if (formData.proxy_url !== (provider.proxy_url || ""))
					updateData.proxy_url = formData.proxy_url;
				if (formData.enabled !== provider.enabled) updateData.enabled = formData.enabled;
				if (formData.is_backup_provider !== provider.is_backup_provider)
					updateData.is_backup_provider = formData.is_backup_provider;

				await updateProvider.mutateAsync({
					id: provider.id,
					data: updateData,
				});
			}

			onSuccess();
		} catch (error) {
			console.error("Failed to save provider:", error);
			showToast({
				type: "error",
				title: "Save Failed",
				message: "Failed to save provider. Please try again.",
			});
		}
	};

	const isFormValid = formData.host && formData.username && formData.password;
	const isSaving = createProvider.isPending || updateProvider.isPending;

	return (
		<div className="modal modal-open">
			<div className="modal-box max-w-2xl p-4 sm:p-6">
				<h3 className="mb-4 font-bold text-lg">
					{mode === "create" ? "Add New Provider" : "Edit Provider"}
				</h3>

				<form className="space-y-4" onSubmit={(e) => e.preventDefault()}>
					{/* Host */}
					<fieldset className="rounded-lg border border-base-300 p-3 sm:p-4">
						<legend className="px-2 font-medium text-sm">Host *</legend>
						<input
							id="host"
							type="text"
							className="input input-bordered w-full"
							value={formData.host}
							onChange={(e) => handleInputChange("host", e.target.value)}
							placeholder="news.example.com"
							required
						/>
					</fieldset>

					{/* Connection Details */}
					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
						<fieldset className="rounded-lg border border-base-300 p-3 sm:p-4">
							<legend className="px-2 font-medium text-sm">Port</legend>
							<input
								id="port"
								type="number"
								className="input input-bordered w-full"
								value={formData.port}
								onChange={(e) =>
									handleInputChange("port", Number.parseInt(e.target.value, 10) || 119)
								}
								min={1}
								max={65535}
							/>
						</fieldset>

						<fieldset className="rounded-lg border border-base-300 p-3 sm:p-4">
							<legend className="px-2 font-medium text-sm">Connections</legend>
							<input
								id="max_connections"
								type="number"
								className="input input-bordered w-full"
								value={formData.max_connections}
								onChange={(e) =>
									handleInputChange("max_connections", Number.parseInt(e.target.value, 10) || 1)
								}
								min={1}
								max={100}
							/>
						</fieldset>

						<fieldset className="rounded-lg border border-base-300 p-3 sm:col-span-2 sm:p-4 lg:col-span-1">
							<legend className="px-2 font-medium text-sm">Pipeline</legend>
							<input
								id="inflight_requests"
								type="number"
								className="input input-bordered w-full"
								value={formData.inflight_requests}
								onChange={(e) =>
									handleInputChange("inflight_requests", Number.parseInt(e.target.value, 10) || 1)
								}
								min={1}
								max={50}
							/>
							<p className="mt-2 text-[10px] text-base-content/60 sm:text-xs">
								Concurrent requests per connection.
							</p>
						</fieldset>
					</div>

					{/* Authentication */}
					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
						<fieldset className="rounded-lg border border-base-300 p-3 sm:p-4">
							<legend className="px-2 font-medium text-sm">Username *</legend>
							<input
								id="username"
								type="text"
								className="input input-bordered w-full"
								value={formData.username}
								onChange={(e) => handleInputChange("username", e.target.value)}
								required
							/>
						</fieldset>

						<fieldset className="rounded-lg border border-base-300 p-3 sm:p-4">
							<legend className="px-2 font-medium text-sm">Password *</legend>
							<input
								id="password"
								type="password"
								className="input input-bordered w-full"
								value={formData.password}
								onChange={(e) => handleInputChange("password", e.target.value)}
								placeholder={mode === "edit" ? "Keep existing" : ""}
								required={mode === "create"}
							/>
						</fieldset>
					</div>

					{/* Security Settings */}
					<fieldset className="space-y-3 rounded-lg border border-base-300 p-3 sm:p-4">
						<legend className="px-2 font-medium text-sm">Security & Options</legend>

						<label htmlFor="tls" className="label cursor-pointer justify-start gap-4">
							<input
								id="tls"
								type="checkbox"
								className="checkbox checkbox-sm"
								checked={formData.tls}
								onChange={(e) => handleInputChange("tls", e.target.checked)}
							/>
							<span className="label-text">TLS/SSL Encryption</span>
						</label>

						{formData.tls && (
							<label
								htmlFor="insecure_tls"
								className="label ml-6 cursor-pointer justify-start gap-4"
							>
								<input
									id="insecure_tls"
									type="checkbox"
									className="checkbox checkbox-sm"
									checked={formData.insecure_tls}
									onChange={(e) => handleInputChange("insecure_tls", e.target.checked)}
								/>
								<span className="label-text">Skip Verification (Insecure)</span>
							</label>
						)}

						<label
							htmlFor="is_backup_provider"
							className="label cursor-pointer justify-start gap-4"
						>
							<input
								id="is_backup_provider"
								type="checkbox"
								className="checkbox checkbox-sm"
								checked={formData.is_backup_provider}
								onChange={(e) => handleInputChange("is_backup_provider", e.target.checked)}
							/>
							<div>
								<span className="label-text font-medium">Backup Provider</span>
								<div className="text-[10px] text-base-content/60">Only used as a fallback.</div>
							</div>
						</label>
					</fieldset>

					{/* Proxy Settings */}
					<fieldset className="rounded-lg border border-base-300 p-3 sm:p-4">
						<legend className="px-2 font-medium text-sm">Proxy (Optional)</legend>
						<input
							id="proxy_url"
							type="text"
							className="input input-bordered w-full"
							value={formData.proxy_url}
							onChange={(e) => handleInputChange("proxy_url", e.target.value)}
							placeholder="socks5://host:port"
						/>
					</fieldset>

					{/* Connection Test */}
					<div className="space-y-4">
						<div className="flex items-center justify-between gap-4">
							<h4 className="font-semibold text-sm sm:text-base">Test Connection</h4>
							<button
								type="button"
								className="btn btn-sm btn-outline"
								onClick={handleTestConnection}
								disabled={!isFormValid || isTestingConnection}
							>
								{isTestingConnection ? (
									<Loader className="h-4 w-4 animate-spin" />
								) : (
									<Wifi className="h-4 w-4" />
								)}
								Test
							</button>
						</div>

						{connectionTestResult && (
							<div
								className={`alert ${
									connectionTestResult.success ? "alert-success" : "alert-error"
								} py-2`}
							>
								{connectionTestResult.success ? (
									<Check className="h-5 w-5 shrink-0" />
								) : (
									<AlertTriangle className="h-5 w-5 shrink-0" />
								)}
								<div className="break-words text-xs">
									<div className="font-medium">
										{connectionTestResult.success
											? `Success!${connectionTestResult.rttMs ? ` (${connectionTestResult.rttMs}ms)` : ""}`
											: "Failed"}
									</div>
									{connectionTestResult.message && <p>{connectionTestResult.message}</p>}
								</div>
							</div>
						)}
					</div>
				</form>

				<div className="modal-action">
					<button type="button" className="btn btn-ghost btn-sm sm:btn-md" onClick={onCancel}>
						Cancel
					</button>
					<button
						type="button"
						className="btn btn-primary btn-sm sm:btn-md"
						onClick={handleSave}
						disabled={!canSave || isSaving}
					>
						{isSaving ? <Loader className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
						Save
					</button>
				</div>
			</div>
		</div>
	);
}
