import { AlertTriangle, Archive, CheckCircle, Play, Search, XCircle } from "lucide-react";
import { useState } from "react";
import {
	useCancelMetadataMigration,
	useDryRunMetadataMigration,
	useMetadataMigrationStatus,
	useStartMetadataMigration,
} from "../../hooks/useMetadataMigration";
import { LoadingSpinner } from "../ui/LoadingSpinner";

function formatBytes(bytes: number): string {
	if (bytes <= 0) return "0 B";
	const units = ["B", "KB", "MB", "GB", "TB"];
	const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
	return `${(bytes / 1024 ** exponent).toFixed(exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}

export function MetadataMigrationCard() {
	const { data: status, isLoading } = useMetadataMigrationStatus();
	const dryRun = useDryRunMetadataMigration();
	const startMigration = useStartMetadataMigration();
	const cancelMigration = useCancelMetadataMigration();
	const [showConfirm, setShowConfirm] = useState(false);

	const isRunning = status?.is_running ?? false;
	const preview = status?.last_dry_run ?? dryRun.data;
	const lastResult = status?.last_result;
	const progress = status?.progress;
	const percent =
		progress && progress.total_files > 0
			? Math.round((progress.processed_files / progress.total_files) * 100)
			: 0;

	const handleConfirmMigrate = async () => {
		setShowConfirm(false);
		await startMigration.mutateAsync();
	};

	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<h2 className="card-title">
					<Archive className="h-5 w-5" aria-hidden="true" />
					Storage Migration
				</h2>
				<p className="text-base-content/70 text-sm">
					Convert legacy metadata files to the shared NZB store format. Segments move out of each
					<code className="mx-1">.meta</code> file into one compressed
					<code className="mx-1">.nzbz</code> per release, reclaiming disk space.
				</p>

				{isLoading ? (
					<div className="flex justify-center py-6">
						<LoadingSpinner size="md" />
					</div>
				) : (
					<>
						{isRunning && progress && (
							<div className="space-y-2 py-2">
								<div className="flex justify-between text-sm">
									<span>
										Release {progress.processed_groups} of {progress.total_groups}
									</span>
									<span>
										{progress.processed_files} / {progress.total_files} files
									</span>
								</div>
								<progress className="progress progress-primary w-full" value={percent} max={100} />
								{progress.current_release && (
									<p className="truncate text-base-content/60 text-xs">
										{progress.current_release}
									</p>
								)}
							</div>
						)}

						{!isRunning && preview && preview.groups > 0 && (
							<div className="overflow-x-auto py-2">
								<table className="table-sm table">
									<tbody>
										<tr>
											<td>Releases</td>
											<td className="text-right font-mono">{preview.groups}</td>
										</tr>
										<tr>
											<td>Files to convert</td>
											<td className="text-right font-mono">{preview.files_migrated}</td>
										</tr>
										<tr>
											<td>Current size</td>
											<td className="text-right font-mono">{formatBytes(preview.bytes_before)}</td>
										</tr>
										<tr>
											<td>Projected size</td>
											<td className="text-right font-mono">{formatBytes(preview.bytes_after)}</td>
										</tr>
										<tr className="font-semibold">
											<td>Reclaimed</td>
											<td className="text-right font-mono text-success">
												{formatBytes(preview.bytes_saved)}
											</td>
										</tr>
										{preview.files_failed > 0 && (
											<tr>
												<td>Cannot convert</td>
												<td className="text-right font-mono text-warning">
													{preview.files_failed}
												</td>
											</tr>
										)}
									</tbody>
								</table>
							</div>
						)}

						{!isRunning && preview && preview.groups === 0 && (
							<div className="alert alert-success">
								<CheckCircle className="h-6 w-6" aria-hidden="true" />
								<div>All metadata is already in the shared store format.</div>
							</div>
						)}

						{lastResult && !isRunning && (
							<div
								className={`alert ${lastResult.files_failed > 0 ? "alert-warning" : "alert-success"}`}
							>
								{lastResult.files_failed > 0 ? (
									<AlertTriangle className="h-6 w-6" aria-hidden="true" />
								) : (
									<CheckCircle className="h-6 w-6" aria-hidden="true" />
								)}
								<div>
									<div className="font-bold">
										{lastResult.cancelled ? "Migration cancelled" : "Migration complete"}
									</div>
									<div className="text-sm">
										{lastResult.files_migrated} files migrated,{" "}
										{formatBytes(lastResult.bytes_saved)} reclaimed
										{lastResult.files_failed > 0 && `, ${lastResult.files_failed} left unchanged`}
									</div>
									{lastResult.failures && lastResult.failures.length > 0 && (
										<details className="mt-2">
											<summary className="cursor-pointer text-sm">
												Show {lastResult.failures.length} file
												{lastResult.failures.length === 1 ? "" : "s"} left in the old format
											</summary>
											<ul className="mt-2 max-h-48 space-y-1 overflow-y-auto font-mono text-xs">
												{lastResult.failures.map((failure) => (
													<li key={failure} className="break-all">
														{failure}
													</li>
												))}
											</ul>
										</details>
									)}
								</div>
							</div>
						)}

						{dryRun.isError && (
							<div className="alert alert-error">
								<XCircle className="h-6 w-6" aria-hidden="true" />
								<div>Dry run failed. Check the logs for details.</div>
							</div>
						)}

						<div className="card-actions justify-end pt-2">
							{isRunning ? (
								<button
									type="button"
									className="btn btn-error btn-outline"
									onClick={() => cancelMigration.mutate()}
									disabled={cancelMigration.isPending}
								>
									<XCircle className="h-4 w-4" aria-hidden="true" />
									Cancel
								</button>
							) : (
								<>
									<button
										type="button"
										className="btn btn-outline"
										onClick={() => dryRun.mutate()}
										disabled={dryRun.isPending}
									>
										{dryRun.isPending ? (
											<LoadingSpinner size="sm" />
										) : (
											<Search className="h-4 w-4" aria-hidden="true" />
										)}
										{dryRun.isPending ? "Scanning..." : "Dry Run"}
									</button>
									<button
										type="button"
										className="btn btn-primary"
										onClick={() => setShowConfirm(true)}
										disabled={!preview || preview.groups === 0 || startMigration.isPending}
									>
										<Play className="h-4 w-4" aria-hidden="true" />
										Migrate
									</button>
								</>
							)}
						</div>
					</>
				)}
			</div>

			{showConfirm && (
				<dialog className="modal modal-open">
					<div className="modal-box">
						<h3 className="font-bold text-lg">Migrate metadata?</h3>
						<p className="py-4">
							This rewrites every legacy <code>.meta</code> file in place and moves its segments
							into a shared <code>.nzbz</code> store per release. The change is one-way. Streaming
							keeps working throughout, and you can cancel at any point.
						</p>
						<div className="modal-action">
							<button type="button" className="btn" onClick={() => setShowConfirm(false)}>
								Cancel
							</button>
							<button type="button" className="btn btn-primary" onClick={handleConfirmMigrate}>
								Migrate
							</button>
						</div>
					</div>
				</dialog>
			)}
		</div>
	);
}
