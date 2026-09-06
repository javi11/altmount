import { Trash2 } from "lucide-react";
import { useState } from "react";
import {
	useCorruptedMetadataStats,
	usePurgeCorruptedMetadata,
} from "../../hooks/useCorruptedMetadata";
import { ConfirmModal } from "../ui/ConfirmModal";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface CorruptedMetadataCardProps {
	isReadOnly?: boolean;
}

function formatBytes(bytes: number): string {
	if (bytes <= 0) return "0 B";
	const units = ["B", "KB", "MB", "GB", "TB"];
	const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
	return `${(bytes / 1024 ** exponent).toFixed(exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}

export function CorruptedMetadataCard({ isReadOnly = false }: CorruptedMetadataCardProps) {
	const { data: stats, isLoading, error } = useCorruptedMetadataStats();
	const purge = usePurgeCorruptedMetadata();
	const [showConfirm, setShowConfirm] = useState(false);

	const fileCount = stats?.file_count ?? 0;

	const handleConfirmPurge = async () => {
		setShowConfirm(false);
		await purge.mutateAsync();
	};

	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<h2 className="card-title">
					<Trash2 className="h-5 w-5" aria-hidden="true" />
					Corrupted Safety Copies
				</h2>
				<p className="text-base-content/70 text-sm">
					When a file is marked corrupted its metadata is moved to
					<code className="mx-1">corrupted_metadata</code> instead of being deleted. Copies older
					than the retention window above are pruned automatically; purge removes all of them now
					and releases the segment stores they hold.
				</p>

				{isLoading ? (
					<div className="flex justify-center py-6">
						<LoadingSpinner size="md" />
					</div>
				) : error ? (
					<div className="alert alert-error">
						<span className="text-sm">Failed to read corrupted metadata stats.</span>
					</div>
				) : (
					<div className="stats stats-vertical sm:stats-horizontal bg-base-200">
						<div className="stat">
							<div className="stat-title">Retained Copies</div>
							<div className="stat-value text-2xl">{fileCount}</div>
						</div>
						<div className="stat">
							<div className="stat-title">On Disk</div>
							<div className="stat-value text-2xl">{formatBytes(stats?.total_bytes ?? 0)}</div>
						</div>
					</div>
				)}

				<div className="card-actions justify-end">
					<button
						type="button"
						className="btn btn-error"
						onClick={() => setShowConfirm(true)}
						disabled={isReadOnly || isLoading || fileCount === 0 || purge.isPending}
					>
						{purge.isPending ? (
							<LoadingSpinner size="sm" />
						) : (
							<Trash2 className="h-4 w-4" aria-hidden="true" />
						)}
						Purge Now
					</button>
				</div>
			</div>

			<ConfirmModal
				isOpen={showConfirm}
				title="Purge corrupted metadata?"
				message={`This permanently removes ${fileCount} safety ${
					fileCount === 1 ? "copy" : "copies"
				} (${formatBytes(stats?.total_bytes ?? 0)}). Files you have not re-imported cannot be recovered from them afterwards.`}
				type="error"
				confirmText="Purge"
				confirmButtonClass="btn-error"
				onConfirm={handleConfirmPurge}
				onCancel={() => setShowConfirm(false)}
			/>
		</div>
	);
}
