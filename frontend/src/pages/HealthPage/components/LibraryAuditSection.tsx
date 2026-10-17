import { AlertTriangle, CheckCircle, FileX, ScanSearch, Unlink, XCircle } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useConfirm } from "../../../contexts/ModalContext";
import { useLibraryAuditCleanup, useLibraryAuditScan } from "../../../hooks/useApi";
import { formatBytes, formatRelativeTime } from "../../../lib/utils";
import type { LibraryAudit, LibraryAuditCleanupResult, LibraryAuditItem } from "../../../types/api";

const GRACE_OPTIONS = [
	{ hours: 0, label: "No grace period" },
	{ hours: 6, label: "6 hours" },
	{ hours: 24, label: "24 hours" },
	{ hours: 72, label: "3 days" },
	{ hours: 168, label: "7 days" },
];

interface AuditListProps {
	title: string;
	description: string;
	icon: typeof FileX;
	items: LibraryAuditItem[];
	selected: Set<string>;
	onToggle: (path: string) => void;
	onToggleAll: (select: boolean) => void;
	renderDetail: (item: LibraryAuditItem) => React.ReactNode;
	actionLabel: string;
	onAction: () => void;
	actionDisabled?: boolean;
	blockedReason?: string;
}

function AuditList({
	title,
	description,
	icon: Icon,
	items,
	selected,
	onToggle,
	onToggleAll,
	renderDetail,
	actionLabel,
	onAction,
	actionDisabled,
	blockedReason,
}: AuditListProps) {
	const allSelected = items.length > 0 && items.every((item) => selected.has(item.path));
	return (
		<section
			className="flex min-w-0 flex-col rounded-lg border border-base-300 bg-base-200/40"
			aria-label={title}
		>
			<header className="flex items-start justify-between gap-2 border-base-300 border-b p-3">
				<div className="min-w-0">
					<h4 className="flex items-center gap-2 font-semibold text-sm">
						<Icon className="h-4 w-4 shrink-0" aria-hidden="true" />
						{title}
						<span
							className={`badge badge-sm ${items.length > 0 ? "badge-warning" : "badge-success"}`}
						>
							{items.length}
						</span>
					</h4>
					<p className="mt-1 text-base-content/60 text-xs">{description}</p>
				</div>
				{items.length > 0 && (
					<label className="flex shrink-0 cursor-pointer items-center gap-2 text-xs">
						<input
							type="checkbox"
							className="checkbox checkbox-xs"
							checked={allSelected}
							onChange={(e) => onToggleAll(e.target.checked)}
						/>
						All
					</label>
				)}
			</header>
			{items.length === 0 ? (
				<p className="flex items-center gap-2 p-4 text-base-content/60 text-sm">
					<CheckCircle className="h-4 w-4 text-success" aria-hidden="true" />
					Nothing found.
				</p>
			) : (
				<ul className="max-h-80 divide-y divide-base-300 overflow-y-auto">
					{items.map((item) => (
						<li key={item.path}>
							<label className="flex cursor-pointer items-start gap-2 px-3 py-2 hover:bg-base-300/40">
								<input
									type="checkbox"
									className="checkbox checkbox-xs mt-0.5"
									checked={selected.has(item.path)}
									onChange={() => onToggle(item.path)}
								/>
								<span className="min-w-0 flex-1">
									<span className="block break-all font-mono text-xs" title={item.path}>
										{item.location === "import" && (
											<span className="badge badge-outline badge-xs mr-1 align-middle font-sans">
												import folder
											</span>
										)}
										{item.path}
									</span>
									<span className="block break-all text-base-content/50 text-xs">
										{renderDetail(item)}
									</span>
								</span>
							</label>
						</li>
					))}
				</ul>
			)}
			{items.length > 0 && (
				<footer className="mt-auto space-y-2 border-base-300 border-t p-3">
					{blockedReason && (
						<p className="flex items-start gap-2 text-error text-xs">
							<XCircle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
							{blockedReason}
						</p>
					)}
					<button
						type="button"
						className="btn btn-error btn-sm"
						onClick={onAction}
						disabled={actionDisabled || selected.size === 0 || Boolean(blockedReason)}
					>
						{actionLabel}
					</button>
				</footer>
			)}
		</section>
	);
}

export function LibraryAuditSection() {
	const [graceHours, setGraceHours] = useState(24);
	const [audit, setAudit] = useState<LibraryAudit | null>(null);
	const [selectedOrphans, setSelectedOrphans] = useState<Set<string>>(new Set());
	const [selectedDead, setSelectedDead] = useState<Set<string>>(new Set());
	const [cleanupResult, setCleanupResult] = useState<LibraryAuditCleanupResult | null>(null);
	const [error, setError] = useState<string | null>(null);
	const scan = useLibraryAuditScan();
	const cleanup = useLibraryAuditCleanup();
	const { confirmAction } = useConfirm();

	const showAudit = (next: LibraryAudit) => {
		setAudit(next);
		// Everything found starts selected; untick what should stay.
		setSelectedOrphans(new Set(next.orphans.map((o) => o.path)));
		setSelectedDead(new Set(next.dead_symlinks.map((d) => d.path)));
	};

	const runScan = async () => {
		setError(null);
		setCleanupResult(null);
		try {
			showAudit(await scan.mutateAsync(graceHours));
		} catch (err) {
			setError(err instanceof Error ? err.message : "Library audit failed");
		}
	};

	const runCleanup = async (orphans: string[], deadSymlinks: string[]) => {
		setError(null);
		try {
			const result = await cleanup.mutateAsync({
				orphans,
				dead_symlinks: deadSymlinks,
				grace_hours: graceHours,
			});
			setCleanupResult(result);
			showAudit(result.audit);
		} catch (err) {
			setError(err instanceof Error ? err.message : "Cleanup failed");
		}
	};

	const cleanOrphans = async () => {
		const paths = [...selectedOrphans];
		const confirmed = await confirmAction(
			"Delete orphaned files from AltMount",
			`This permanently deletes ${paths.length} file${paths.length === 1 ? "" : "s"} from AltMount (metadata and health records). No library symlink points at ${paths.length === 1 ? "it" : "them"}, so nothing in your library will break, but ${paths.length === 1 ? "it" : "they"} cannot be streamed again without re-importing.`,
			{
				type: "error",
				confirmText: "Delete files",
				confirmButtonClass: "btn-error",
				verificationText: "DELETE",
			},
		);
		if (confirmed) await runCleanup(paths, []);
	};

	const cleanDead = async () => {
		const paths = [...selectedDead];
		const confirmed = await confirmAction(
			"Remove dead symlinks",
			`This removes ${paths.length} symlink${paths.length === 1 ? "" : "s"} from your library whose AltMount file no longer exists. Your media server will drop ${paths.length === 1 ? "that entry" : "those entries"} on its next scan, and Sonarr/Radarr will show ${paths.length === 1 ? "it" : "them"} as missing.`,
			{ type: "warning", confirmText: "Remove symlinks", confirmButtonClass: "btn-warning" },
		);
		if (confirmed) await runCleanup([], paths);
	};

	const toggle = (set: Set<string>, setter: (s: Set<string>) => void) => (path: string) => {
		const next = new Set(set);
		if (next.has(path)) next.delete(path);
		else next.add(path);
		setter(next);
	};

	const busy = scan.isPending || cleanup.isPending;

	return (
		<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
			<div className="card-body space-y-4 p-4 sm:p-6">
				<div>
					<h3 className="card-title text-base">
						<ScanSearch className="h-5 w-5 text-primary" aria-hidden="true" />
						Library Audit
					</h3>
					<p className="mt-1 text-base-content/60 text-sm">
						Compares your library against AltMount: files in AltMount that no library symlink points
						at, and library symlinks whose AltMount file is gone. The scan is a dry run — nothing
						changes until you clean up. It scans the{" "}
						<Link to="/config/health" className="link link-primary">
							Library Parent Directory
						</Link>{" "}
						set in Configuration → Health Monitoring.
					</p>
				</div>

				<div className="flex flex-wrap items-end gap-3">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Ignore files imported within</legend>
						<select
							className="select select-bordered select-sm w-48"
							value={graceHours}
							onChange={(e) => setGraceHours(Number(e.target.value))}
							disabled={busy}
						>
							{GRACE_OPTIONS.map((o) => (
								<option key={o.hours} value={o.hours}>
									{o.label}
								</option>
							))}
						</select>
					</fieldset>
					<button
						type="button"
						className="btn btn-primary btn-sm"
						onClick={runScan}
						disabled={busy}
					>
						{scan.isPending ? (
							<span className="loading loading-spinner loading-xs" aria-hidden="true" />
						) : (
							<ScanSearch className="h-4 w-4" aria-hidden="true" />
						)}
						{audit ? "Scan again" : "Scan (dry run)"}
					</button>
				</div>

				{error && (
					<div className="alert alert-error text-sm">
						<XCircle className="h-5 w-5 shrink-0" aria-hidden="true" />
						<div>{error}</div>
					</div>
				)}

				{cleanupResult && (
					<div className="alert alert-success text-sm">
						<CheckCircle className="h-5 w-5 shrink-0" aria-hidden="true" />
						<div>
							Deleted {cleanupResult.orphans_deleted} orphaned file
							{cleanupResult.orphans_deleted === 1 ? "" : "s"} and removed{" "}
							{cleanupResult.dead_symlinks_removed} dead symlink
							{cleanupResult.dead_symlinks_removed === 1 ? "" : "s"}.
							{cleanupResult.skipped.length > 0 && (
								<details className="mt-1">
									<summary className="cursor-pointer">
										{cleanupResult.skipped.length} skipped
									</summary>
									<ul className="mt-1 list-inside list-disc">
										{cleanupResult.skipped.map((s) => (
											<li key={s.path} className="break-all">
												<span className="font-mono">{s.path}</span> — {s.reason}
											</li>
										))}
									</ul>
								</details>
							)}
						</div>
					</div>
				)}

				{audit && (
					<>
						<p className="text-base-content/60 text-xs">
							Scanned {audit.files_scanned.toLocaleString()} AltMount files and{" "}
							{audit.symlinks_scanned.toLocaleString()} library symlinks in{" "}
							<span className="font-mono">{audit.library_dir}</span>
							{audit.import_dir && (
								<>
									{" "}
									plus {audit.import_symlinks_scanned.toLocaleString()} links in the import folder{" "}
									<span className="font-mono">{audit.import_dir}</span>
								</>
							)}{" "}
							{formatRelativeTime(audit.scanned_at)}.
							{audit.skipped_recent > 0 &&
								` ${audit.skipped_recent} recently imported file${audit.skipped_recent === 1 ? " was" : "s were"} left out (still waiting for Sonarr/Radarr to link them).`}
						</p>
						{audit.warning && (
							<div className="alert alert-warning text-sm">
								<AlertTriangle className="h-5 w-5 shrink-0" aria-hidden="true" />
								<div>{audit.warning}</div>
							</div>
						)}
						<div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
							<AuditList
								title="Orphaned files in AltMount"
								description="No library symlink points at these files."
								icon={FileX}
								items={audit.orphans}
								selected={selectedOrphans}
								onToggle={toggle(selectedOrphans, setSelectedOrphans)}
								onToggleAll={(all) =>
									setSelectedOrphans(all ? new Set(audit.orphans.map((o) => o.path)) : new Set())
								}
								renderDetail={(item) =>
									[
										item.size ? formatBytes(item.size) : null,
										item.created_at ? `imported ${formatRelativeTime(item.created_at)}` : null,
									]
										.filter(Boolean)
										.join(" · ")
								}
								actionLabel={`Delete ${selectedOrphans.size} selected`}
								onAction={cleanOrphans}
								actionDisabled={busy}
								blockedReason={audit.orphan_cleanup_blocked}
							/>
							<AuditList
								title="Dead symlinks"
								description="Symlinks in your library, or in AltMount's import folder, whose AltMount file no longer exists. Removing an import-folder link also removes its release folder if that leaves it empty."
								icon={Unlink}
								items={audit.dead_symlinks}
								selected={selectedDead}
								onToggle={toggle(selectedDead, setSelectedDead)}
								onToggleAll={(all) =>
									setSelectedDead(all ? new Set(audit.dead_symlinks.map((d) => d.path)) : new Set())
								}
								renderDetail={(item) => (item.target ? `→ ${item.target}` : null)}
								actionLabel={`Remove ${selectedDead.size} selected`}
								onAction={cleanDead}
								actionDisabled={busy}
							/>
						</div>
					</>
				)}
			</div>
		</div>
	);
}
