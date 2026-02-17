import { AlertTriangle, Check, Loader, Save, Wifi } from "lucide-react";
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
				provider_id: provider?.id,
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
		if (mode === "create" && !canSave) {
			showToast({
				type: "warning",
				title: "Connection Test Required",
				message: "Please test the connection successfully before saving a new provider",
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

	const isFormValid = formData.host && formData.username && (mode === "edit" || formData.password);
	const isSaving = createProvider.isPending || updateProvider.isPending;

	return (
		<div className="modal modal-open backdrop-blur-sm">
			<div className="modal-box max-w-2xl rounded-2xl border border-base-300 shadow-2xl">
				<h3 className="mb-6 font-black text-xl uppercase tracking-tighter">
					{mode === "create" ? "Add New Provider" : "Edit Provider"}
				</h3>

				<form className="space-y-6" onSubmit={(e) => e.preventDefault()}>
					{/* Host */}
					<fieldset className="fieldset">
						<legend className="fieldset-legend font-bold">NNTP Host *</legend>
						<input
							id="host"
							type="text"
							className="input input-bordered w-full font-mono text-sm"
							value={formData.host}
							onChange={(e) => handleInputChange("host", e.target.value)}
							placeholder="news.example.com"
							required
						/>
					</fieldset>

					{/* Connection Details */}
					<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-bold">Port</legend>
							<input
								id="port"
								type="number"
								className="input input-bordered w-full font-mono text-sm"
								value={formData.port}
								onChange={(e) =>
									handleInputChange("port", Number.parseInt(e.target.value, 10) || 119)
								}
								min={1}
								max={65535}
							/>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend font-bold">Max Connections</legend>
							<input
								id="max_connections"
								type="number"
								className="input input-bordered w-full font-mono text-sm"
								value={formData.max_connections}
								onChange={(e) =>
									handleInputChange("max_connections", Number.parseInt(e.target.value, 10) || 1)
								}
								min={1}
								max={100}
							/>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend font-bold">Pipeline (Inflight)</legend>
							<input
								id="inflight_requests"
								type="number"
								className="input input-bordered w-full font-mono text-sm"
								value={formData.inflight_requests}
								onChange={(e) =>
									handleInputChange("inflight_requests", Number.parseInt(e.target.value, 10) || 1)
								}
								min={1}
								max={100}
							/>
							<p className="label mt-1 text-[10px] opacity-50">
								Requests per connection. Default is 10.
							</p>
						</fieldset>
					</div>

					{/* Authentication */}
					<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-bold">Username *</legend>
							<input
								id="username"
								type="text"
								className="input input-bordered w-full font-mono text-sm"
								value={formData.username}
								onChange={(e) => handleInputChange("username", e.target.value)}
								required
							/>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend font-bold">
								Password {mode === "create" ? "*" : ""}
							</legend>
							<input
								id="password"
								type="password"
								className="input input-bordered w-full font-mono text-sm"
								value={formData.password}
								onChange={(e) => handleInputChange("password", e.target.value)}
								placeholder={mode === "edit" ? "••••••••••••••••" : ""}
								required={mode === "create"}
							/>
							{mode === "edit" && (
								<p className="label text-[10px] opacity-50">Leave empty to keep current.</p>
							)}
						</fieldset>
					</div>

					{/* Security Settings */}
					<div className="space-y-4 rounded-2xl border border-base-300 bg-base-200/30 p-5">
						<h4 className="font-bold text-[10px] uppercase tracking-widest opacity-40">
							Options & Security
						</h4>

						<div className="flex flex-col gap-4">
							<label htmlFor="tls" className="label cursor-pointer items-start justify-start gap-3">
								<input
									id="tls"
									type="checkbox"
									className="checkbox checkbox-primary checkbox-sm mt-0.5"
									checked={formData.tls}
									onChange={(e) => handleInputChange("tls", e.target.checked)}
								/>
								<div className="min-w-0 flex-1">
									<span className="label-text font-bold text-xs">Use SSL/TLS</span>
									<span className="block text-[10px] opacity-50">
										Highly recommended for privacy.
									</span>
								</div>
							</label>

							{formData.tls && (
								<label
									htmlFor="insecure_tls"
									className="label ml-7 cursor-pointer items-start justify-start gap-3"
								>
									<input
										id="insecure_tls"
										type="checkbox"
										className="checkbox checkbox-warning checkbox-sm mt-0.5"
										checked={formData.insecure_tls}
										onChange={(e) => handleInputChange("insecure_tls", e.target.checked)}
									/>
									<div className="min-w-0 flex-1">
										<span className="label-text font-bold text-xs">
											Insecure (Skip Verification)
										</span>
										<span className="block text-[10px] opacity-50">
											Only use for self-signed certs.
										</span>
									</div>
								</label>
							)}

							<label
								htmlFor="is_backup_provider"
								className="label cursor-pointer items-start justify-start gap-3"
							>
								<input
									id="is_backup_provider"
									type="checkbox"
									className="checkbox checkbox-primary checkbox-sm mt-0.5"
									checked={formData.is_backup_provider}
									onChange={(e) => handleInputChange("is_backup_provider", e.target.checked)}
								/>
								<div className="min-w-0 flex-1">
									<span className="label-text font-bold text-xs">Backup Only</span>
									<span className="block text-[10px] opacity-50">
										Only use when primary providers fail.
									</span>
								</div>
							</label>
						</div>
					</div>

					{/* Proxy Settings */}
					<fieldset className="fieldset">
						<legend className="fieldset-legend font-bold">SOCKS5 Proxy (Optional)</legend>
						<input
							id="proxy_url"
							type="text"
							className="input input-bordered w-full font-mono text-sm"
							value={formData.proxy_url}
							onChange={(e) => handleInputChange("proxy_url", e.target.value)}
							placeholder="socks5://user:pass@host:port"
						/>
					</fieldset>

					{/* Connection Test */}
					<div className="space-y-4 border-base-300/50 border-t pt-4">
						<div className="flex items-center justify-between">
							<h4 className="font-bold text-xs uppercase tracking-widest opacity-40">
								Connectivity Check
							</h4>
							<button
								type="button"
								className="btn btn-xs btn-outline px-4"
								onClick={handleTestConnection}
								disabled={!isFormValid || isTestingConnection}
							>
								{isTestingConnection ? (
									<Loader className="h-3 w-3 animate-spin" />
								) : (
									<Wifi className="h-3 w-3" />
								)}
								Test Link
							</button>
						</div>

						{connectionTestResult && (
							<div
								className={`alert rounded-xl py-2 text-xs ${
									connectionTestResult.success
										? "alert-success border-success/20 bg-success/10 text-success"
										: "alert-error border-error/20 bg-error/10 text-error"
								}`}
							>
								{connectionTestResult.success ? (
									<Check className="h-4 w-4" />
								) : (
									<AlertTriangle className="h-4 w-4" />
								)}
								<div>
									<div className="font-black text-[10px] uppercase tracking-widest">
										{connectionTestResult.success
											? `Success${connectionTestResult.rttMs !== undefined ? ` • ${connectionTestResult.rttMs}ms` : ""}`
											: "Failed"}
									</div>
									{connectionTestResult.message && (
										<div className="mt-0.5 font-medium">{connectionTestResult.message}</div>
									)}
								</div>
							</div>
						)}
					</div>
				</form>

				<div className="modal-action gap-3">
					<button type="button" className="btn btn-ghost" onClick={onCancel}>
						Cancel
					</button>
					<button
						type="button"
						className="btn btn-primary px-8 shadow-lg shadow-primary/20"
						onClick={handleSave}
						disabled={isSaving || (mode === "create" && !canSave)}
					>
						{isSaving ? <Loader className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
						{mode === "create" ? "Create Provider" : "Save Changes"}
					</button>
				</div>
			</div>
		</div>
	);
}
