import { Globe } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, NetworkConfig } from "../../types/config";

interface NetworkConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: NetworkConfig) => Promise<void>;
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
	const baseline = config.network ?? emptyNetwork;
	const [data, setData] = useState<NetworkConfig>(baseline);
	const [hasChanges, setHasChanges] = useState(false);

	useEffect(() => {
		setData(config.network ?? emptyNetwork);
		setHasChanges(false);
	}, [config.network]);

	const handleChange = (field: keyof NetworkConfig, value: string) => {
		const next: NetworkConfig = { ...data, [field]: value };
		setData(next);
		setHasChanges(JSON.stringify(next) !== JSON.stringify(baseline));
	};

	const handleSave = async () => {
		if (!onUpdate || !hasChanges) return;
		await onUpdate("network", data);
		setHasChanges(false);
	};

	return (
		<div className="space-y-6">
			<div className="flex items-center gap-2">
				<Globe className="h-5 w-5" aria-hidden="true" />
				<h2 className="font-semibold text-xl">Network &amp; Proxy</h2>
			</div>

			<div className="alert alert-info">
				<div className="text-sm">
					Applied to every outbound HTTP request used for indexer search, NZB grabbing, Arrs
					(Radarr/Sonarr/Lidarr/Readarr/Whisparr), SABnzbd fallback, and the NZBLNK resolver.
					Internal endpoints (RC server, self-loopback) are not affected. Leave fields blank to
					connect directly. Changes take effect on the next external request — restart AltMount if
					you want long-lived clients to pick up the new proxy immediately.
				</div>
			</div>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">HTTP Proxy</legend>
				<input
					type="text"
					className="input"
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
					className="input"
					placeholder="http://user:pass@host:3128"
					value={data.https_proxy}
					disabled={isReadOnly}
					onChange={(e) => handleChange("https_proxy", e.target.value)}
				/>
				<p className="label">Used for HTTPS outbound requests. May be the same as HTTP Proxy.</p>
			</fieldset>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">No Proxy</legend>
				<input
					type="text"
					className="input"
					placeholder="localhost,127.0.0.1,10.0.0.0/8,*.internal"
					value={data.no_proxy}
					disabled={isReadOnly}
					onChange={(e) => handleChange("no_proxy", e.target.value)}
				/>
				<p className="label">Comma-separated hosts, IPs, or CIDRs that bypass the proxy.</p>
			</fieldset>

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
