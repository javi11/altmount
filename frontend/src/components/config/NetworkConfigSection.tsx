import { useEffect, useState } from "react";
import type { ConfigResponse, NetworkConfig } from "../../types/config";

interface NetworkConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: NetworkConfig | string) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

const emptyNetwork: NetworkConfig = {
	http_proxy: "",
	https_proxy: "",
	no_proxy: "",
};

export function NetworkConfigSection({
	config,
	onUpdate,
	isReadOnly,
	isUpdating,
}: NetworkConfigSectionProps) {
	const [data, setData] = useState<NetworkConfig>(config.network ?? emptyNetwork);
	const [userAgent, setUserAgent] = useState<string>(config.user_agent ?? "");
	const [hasChanges, setHasChanges] = useState(false);

	useEffect(() => {
		setData(config.network ?? emptyNetwork);
		setUserAgent(config.user_agent ?? "");
		setHasChanges(false);
	}, [config.network, config.user_agent]);

	const recomputeChanges = (nextNetwork: NetworkConfig, nextUserAgent: string) => {
		const networkChanged =
			JSON.stringify(nextNetwork) !== JSON.stringify(config.network ?? emptyNetwork);
		const userAgentChanged = nextUserAgent !== (config.user_agent ?? "");
		setHasChanges(networkChanged || userAgentChanged);
	};

	const handleChange = (field: keyof NetworkConfig, value: string) => {
		const next: NetworkConfig = { ...data, [field]: value };
		setData(next);
		recomputeChanges(next, userAgent);
	};

	const handleUserAgentChange = (value: string) => {
		setUserAgent(value);
		recomputeChanges(data, value);
	};

	const handleSave = async () => {
		if (!onUpdate || !hasChanges) return;
		// Capture locally so a refetch-triggered state reset between the two
		// per-section PATCH calls can't drop the pending edits.
		const networkToSave = data;
		const userAgentToSave = userAgent;
		if (JSON.stringify(networkToSave) !== JSON.stringify(config.network ?? emptyNetwork)) {
			await onUpdate("network", networkToSave);
		}
		if (userAgentToSave !== (config.user_agent ?? "")) {
			await onUpdate("user_agent", userAgentToSave);
		}
		setHasChanges(false);
	};

	return (
		<div className="space-y-6">
			<div className="alert alert-info">
				<div className="text-sm">
					Applied to every outbound HTTP request used for indexer search, NZB grabbing, Arrs
					(Radarr/Sonarr/Lidarr/Readarr/Whisparr), SABnzbd fallback, and the NZBLNK resolver.
					Internal endpoints (RC server, self-loopback) are not affected. Leave fields blank to
					connect directly. Changes take effect on the next external request. Restart AltMount if
					you want long-lived clients to pick up the new proxy immediately.
				</div>
			</div>

			<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">HTTP Proxy</legend>
					<input
						type="text"
						className="input w-full"
						placeholder="http://user:pass@host:3128"
						value={data.http_proxy}
						disabled={isReadOnly}
						onChange={(e) => handleChange("http_proxy", e.target.value)}
					/>
					<p className="label">Used for plain HTTP outbound requests.</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">HTTPS Proxy</legend>
					<input
						type="text"
						className="input w-full"
						placeholder="http://user:pass@host:3128"
						value={data.https_proxy}
						disabled={isReadOnly}
						onChange={(e) => handleChange("https_proxy", e.target.value)}
					/>
					<p className="label">Used for HTTPS outbound requests. May be the same as HTTP Proxy.</p>
				</fieldset>
			</div>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">No Proxy</legend>
				<input
					type="text"
					className="input w-full"
					placeholder="localhost,127.0.0.1,10.0.0.0/8,*.internal"
					value={data.no_proxy}
					disabled={isReadOnly}
					onChange={(e) => handleChange("no_proxy", e.target.value)}
				/>
				<p className="label">Comma-separated hosts, IPs, or CIDRs that bypass the proxy.</p>
			</fieldset>

			<div className="min-w-0 space-y-4 border-base-200 border-t pt-6">
				<div className="min-w-0">
					<h3 className="font-bold text-base-content text-lg tracking-tight">HTTP User-Agent</h3>
					<p className="break-words text-base-content/50 text-sm">
						Global User-Agent sent on outbound HTTP requests (NZB downloads from indexers, NZBLNK
						resolution, metadata lookups).
					</p>
				</div>

				<fieldset className="fieldset min-w-0">
					<legend className="fieldset-legend font-semibold">User-Agent</legend>
					<input
						type="text"
						className="input input-bordered w-full min-w-0 max-w-full bg-base-100 font-mono text-sm"
						value={userAgent}
						disabled={isReadOnly}
						placeholder="Mozilla/5.0 ... (leave empty for default)"
						onChange={(e) => handleUserAgentChange(e.target.value)}
					/>
					<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/70 text-xs">
						Some public indexers (e.g. nzbking.com, nzbindex.com) reject non-browser agents. Leave
						empty to use the built-in browser-like default.
					</p>
				</fieldset>
			</div>

			<button
				type="button"
				className="btn btn-primary"
				onClick={handleSave}
				disabled={!hasChanges || isUpdating || isReadOnly}
			>
				{isUpdating ? "Saving..." : "Save Changes"}
			</button>
		</div>
	);
}
