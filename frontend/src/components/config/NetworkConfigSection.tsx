import { useEffect, useState } from "react";
import type { ConfigResponse, NetworkConfig, NzblnkConfig } from "../../types/config";

interface NetworkConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: NetworkConfig | NzblnkConfig) => Promise<void>;
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
	const [nzblnk, setNzblnk] = useState<NzblnkConfig>(config.nzblnk ?? {});
	const [hasChanges, setHasChanges] = useState(false);

	useEffect(() => {
		setData(config.network ?? emptyNetwork);
		setNzblnk(config.nzblnk ?? {});
		setHasChanges(false);
	}, [config.network, config.nzblnk]);

	const recomputeChanges = (nextNetwork: NetworkConfig, nextNzblnk: NzblnkConfig) => {
		const networkChanged =
			JSON.stringify(nextNetwork) !== JSON.stringify(config.network ?? emptyNetwork);
		const nzblnkChanged = JSON.stringify(nextNzblnk) !== JSON.stringify(config.nzblnk ?? {});
		setHasChanges(networkChanged || nzblnkChanged);
	};

	const handleChange = (field: keyof NetworkConfig, value: string) => {
		const next: NetworkConfig = { ...data, [field]: value };
		setData(next);
		recomputeChanges(next, nzblnk);
	};

	const handleNzblnkChange = (field: keyof NzblnkConfig, value: string) => {
		const next: NzblnkConfig = { ...nzblnk, [field]: value };
		setNzblnk(next);
		recomputeChanges(data, next);
	};

	const handleSave = async () => {
		if (!onUpdate || !hasChanges) return;
		// Capture locally so a refetch-triggered state reset between the two
		// per-section PATCH calls can't drop the pending edits.
		const networkToSave = data;
		const nzblnkToSave = nzblnk;
		if (JSON.stringify(networkToSave) !== JSON.stringify(config.network ?? emptyNetwork)) {
			await onUpdate("network", networkToSave);
		}
		if (JSON.stringify(nzblnkToSave) !== JSON.stringify(config.nzblnk ?? {})) {
			await onUpdate("nzblnk", nzblnkToSave);
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

			{/* NZBLNK Resolver (merged from the former NZBLNK section) */}
			<div className="min-w-0 space-y-4 border-base-200 border-t pt-6">
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
							value={nzblnk.user_agent ?? ""}
							disabled={isReadOnly}
							placeholder="Mozilla/5.0 ... (leave empty for default)"
							onChange={(e) => handleNzblnkChange("user_agent", e.target.value)}
						/>
						<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/70 text-xs">
							HTTP User-Agent sent when searching and downloading from public NZB indexers (e.g.
							nzbking.com, nzbindex.com). Leave empty to use the built-in default.
						</p>
					</fieldset>
				</div>
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
