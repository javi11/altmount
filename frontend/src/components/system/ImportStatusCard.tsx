import { Database, Download, FolderOpen, Loader2 } from "lucide-react";
import { useMemo } from "react";
import { useNzbdavImportStatus, useQueue, useQueueStats, useScanStatus } from "../../hooks/useApi";
import { useProgressStream } from "../../hooks/useProgressStream";
import { ScanStatus } from "../../types/api";

interface ImportStatusCardProps {
	className?: string;
}

export function ImportStatusCard({ className }: ImportStatusCardProps) {
	const { data: scanStatus } = useScanStatus(5000);
	const { data: nzbDavStatus } = useNzbdavImportStatus(5000);
	const { data: queueStats } = useQueueStats();
	const { data: processingQueue } = useQueue({ status: "processing", limit: 5 });

	const hasProcessingItems = (processingQueue?.data?.length || 0) > 0;
	const { progress: liveProgress } = useProgressStream({ enabled: hasProcessingItems });

	const activeImport = useMemo(() => {
		// 1. Check for directory scan
		if (scanStatus?.status === ScanStatus.SCANNING) {
			const percent =
				scanStatus.files_found > 0
					? Math.round((scanStatus.files_added / scanStatus.files_found) * 100)
					: 0;
			return {
				title: "Directory Scan",
				icon: <FolderOpen className="h-8 w-8 text-primary" />,
				progress: percent,
				detail: `${scanStatus.files_added} / ${scanStatus.files_found} files`,
				status: "Scanning",
				color: "progress-primary",
			};
		}

		// 2. Check for NZBDav import
		if (nzbDavStatus?.status === "running") {
			const processed =
				(nzbDavStatus.added || 0) + (nzbDavStatus.failed || 0) + (nzbDavStatus.skipped || 0);
			const percent =
				nzbDavStatus.total > 0 ? Math.round((processed / nzbDavStatus.total) * 100) : 0;
			return {
				title: "NZBDav Import",
				icon: <Database className="h-8 w-8 text-secondary" />,
				progress: percent,
				detail: `${processed} / ${nzbDavStatus.total} items`,
				status: "Importing",
				color: "progress-secondary",
			};
		}

		// 3. Check for active queue processing
		if (processingQueue?.data && processingQueue.data.length > 0) {
			// Calculate aggregate progress of active items
			let totalPercent = 0;
			let count = 0;

			for (const item of processingQueue.data) {
				const percent = liveProgress[item.id] ?? item.percentage ?? 0;
				totalPercent += percent;
				count++;
			}

			const avgPercent = count > 0 ? Math.round(totalPercent / count) : 0;
			const topItem = processingQueue.data[0];
			const displayName = topItem.target_path
				? topItem.target_path.split("/").pop()
				: topItem.nzb_path.split("/").pop();

			return {
				title: "Queue Import",
				icon: <Download className="h-8 w-8 text-info" />,
				progress: avgPercent,
				detail: count > 1 ? `${count} items processing` : displayName,
				status: count > 1 ? `${avgPercent}% (avg)` : `${avgPercent}%`,
				color: "progress-info",
			};
		}

		// Fallback: Overall Queue Summary
		if (queueStats) {
			const totalItems =
				queueStats.total_processing + queueStats.total_completed + queueStats.total_failed;
			const completedAndFailed = queueStats.total_completed + queueStats.total_failed;

			const progressParts: string[] = [];
			if (queueStats.total_queued > 0) progressParts.push(`${queueStats.total_queued} pending`);
			if (queueStats.total_failed > 0) progressParts.push(`${queueStats.total_failed} failed`);

			return {
				title: "Import Queue",
				icon: <Download className="h-8 w-8 text-base-content/20" />,
				progress: 0,
				detail: progressParts.length > 0 ? progressParts.join(", ") : "All tasks complete",
				status: totalItems > 0 ? `${completedAndFailed} / ${totalItems}` : "Idle",
				color: "progress-primary",
				isIdle: totalItems === 0 || completedAndFailed === totalItems,
			};
		}

		return null;
	}, [scanStatus, nzbDavStatus, processingQueue, liveProgress, queueStats]);

	if (!activeImport) {
		return (
			<div className={`card bg-base-100 shadow-lg ${className || ""}`}>
				<div className="card-body">
					<div className="flex items-center justify-between">
						<div>
							<h2 className="card-title font-medium text-base-content/70 text-sm">Import Status</h2>
							<div className="mt-1 flex items-center gap-2">
								<Loader2 className="h-4 w-4 animate-spin text-base-content/20" />
								<div className="text-base-content/30 text-sm italic">Loading...</div>
							</div>
						</div>
						<Download className="h-8 w-8 text-base-content/10" />
					</div>
				</div>
			</div>
		);
	}

	return (
		<div className={`card bg-base-100 shadow-lg ${className || ""}`}>
			<div className="card-body">
				<div className="flex items-start justify-between">
					<div className="min-w-0 flex-1">
						<h2 className="card-title truncate font-medium text-base-content/70 text-sm">
							{activeImport.title}
						</h2>
						<div className="flex items-baseline gap-2">
							<div className="font-bold text-2xl">{activeImport.status}</div>
						</div>
					</div>
					<div className="shrink-0">{activeImport.icon}</div>
				</div>

				<div className="mt-4">
					{!activeImport.isIdle && (
						<>
							<div className="mb-1 flex justify-between font-bold text-[10px] uppercase tracking-wider opacity-60">
								<span className="mr-2 truncate">{activeImport.detail}</span>
								{activeImport.progress > 0 && <span>{activeImport.progress}%</span>}
							</div>
							<progress
								className={`progress ${activeImport.color} h-1.5 w-full`}
								value={activeImport.progress > 0 ? activeImport.progress : undefined}
								max="100"
							/>
						</>
					)}
					{activeImport.isIdle && (
						<div className="flex items-center gap-1.5 font-bold text-[10px] text-base-content/30 uppercase tracking-widest">
							<div className="h-1.5 w-1.5 rounded-full bg-base-content/20" />
							{activeImport.detail}
						</div>
					)}
				</div>
			</div>
		</div>
	);
}
