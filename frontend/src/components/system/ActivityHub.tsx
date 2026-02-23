import { CheckCircle2, Download, FileVideo, History, Play } from "lucide-react";
import { useMemo, useState } from "react";
import { useActiveStreams, useImportHistory, useQueue } from "../../hooks/useApi";
import { useProgressStream } from "../../hooks/useProgressStream";
import { formatBytes, formatDuration, formatRelativeTime, formatSpeed } from "../../lib/utils";
import type { ActiveStream } from "../../types/api";
import { LoadingSpinner } from "../ui/LoadingSpinner";

export function ActivityHub() {
	const [activeTab, setActiveTab] = useState<"playback" | "imports" | "history">("playback");
	const { data: allStreams, isLoading: streamsLoading } = useActiveStreams();
	const { data: queueResponse, isLoading: queueLoading } = useQueue({
		status: "processing",
		limit: 10,
	});
	const { data: importHistory, isLoading: historyLoading } = useImportHistory(
		20,
		activeTab === "history" ? 10000 : 60000,
	);

	const queueItems = queueResponse?.data;
	const hasProcessingItems = (queueItems?.length || 0) > 0;
	const { progress: liveProgress } = useProgressStream({ enabled: hasProcessingItems });

	// Enrich queue items with live progress
	const enrichedQueueItems = useMemo(() => {
		if (!queueItems) return [];
		return queueItems.map((item) => ({
			...item,
			percentage: liveProgress[item.id] ?? item.percentage,
		}));
	}, [queueItems, liveProgress]);

	// Group streams by file_path to show "unique playback sessions"
	const groupedStreams = useMemo(() => {
		if (!allStreams) return [];

		// Filter to show only active streaming sessions (WebDAV or FUSE)
		const streamingOnly = allStreams.filter((s) => {
			const isSystemSource = s.source === "WebDAV" || s.source === "FUSE";
			const isStreaming = s.status === "Streaming";

			// Heuristic: Filter out metadata probes and very short system scans
			// 1. If reading the very end of the file (last 5MB), it's likely a probe
			const isAtEnd = s.total_size > 0 && s.current_offset > s.total_size - 5 * 1024 * 1024;
			// 2. If it's very new and hasn't sent much data yet, hide it briefly
			// (Reduced thresholds to show streams faster)
			const isTooNew = s.bytes_sent < 5 * 1024 * 1024;
			const ageSeconds = (Date.now() - new Date(s.started_at).getTime()) / 1000;

			if (isAtEnd) return false;
			if (isTooNew && ageSeconds < 5) return false;

			return isSystemSource && (isStreaming || s.status === "Buffering");
		});

		const groups: Record<string, ActiveStream> = {};

		for (const stream of streamingOnly) {
			if (!groups[stream.file_path]) {
				groups[stream.file_path] = { ...stream };
			} else {
				// Aggregate data for the same file
				groups[stream.file_path].bytes_sent += stream.bytes_sent;
				groups[stream.file_path].bytes_downloaded += stream.bytes_downloaded;
				groups[stream.file_path].bytes_per_second += stream.bytes_per_second;
				groups[stream.file_path].download_speed += stream.download_speed;
				// Use the highest offset as current position
				if (stream.current_offset > groups[stream.file_path].current_offset) {
					groups[stream.file_path].current_offset = stream.current_offset;
				}
				// Use highest buffered offset
				if (stream.buffered_offset > groups[stream.file_path].buffered_offset) {
					groups[stream.file_path].buffered_offset = stream.buffered_offset;
				}
			}
		}

		return Object.values(groups);
	}, [allStreams]);

	const playbackCount = groupedStreams.length;
	const importCount = queueItems?.length || 0;

	return (
		<div className="card min-h-[400px] bg-base-100 shadow-lg">
			<div className="card-body p-0">
				<div className="tabs tabs-bordered grid w-full grid-cols-3">
					<button
						type="button"
						className={`tab tab-lg gap-2 ${activeTab === "playback" ? "tab-active border-primary font-bold text-primary" : ""}`}
						onClick={() => setActiveTab("playback")}
					>
						<Play className="h-4 w-4" />
						Playback
						{playbackCount > 0 && (
							<span className="badge badge-sm badge-primary">{playbackCount}</span>
						)}
					</button>
					<button
						type="button"
						className={`tab tab-lg gap-2 ${activeTab === "imports" ? "tab-active border-secondary font-bold text-secondary" : ""}`}
						onClick={() => setActiveTab("imports")}
					>
						<Download className="h-4 w-4" />
						Imports
						{importCount > 0 && (
							<span className="badge badge-sm badge-secondary">{importCount}</span>
						)}
					</button>
					<button
						type="button"
						className={`tab tab-lg gap-2 ${activeTab === "history" ? "tab-active border-accent font-bold text-accent" : ""}`}
						onClick={() => setActiveTab("history")}
					>
						<History className="h-4 w-4" />
						History
					</button>
				</div>

				<div className="max-h-[350px] overflow-y-auto p-4">
					{activeTab === "playback" && (
						<div className="space-y-4">
							{streamsLoading ? (
								<div className="flex justify-center py-10">
									<LoadingSpinner />
								</div>
							) : groupedStreams.length > 0 ? (
								groupedStreams.map((stream) => {
									const position =
										stream.current_offset > 0 ? stream.current_offset : stream.bytes_sent;
									const progress =
										stream.total_size > 0 ? Math.round((position / stream.total_size) * 100) : 0;
									const bufferedProgress =
										stream.total_size > 0
											? Math.round((stream.buffered_offset / stream.total_size) * 100)
											: 0;

									return (
										<div
											key={stream.id}
											className="group flex flex-col gap-2 rounded-lg bg-base-200/30 p-3"
										>
											<div className="flex items-center gap-3">
												<FileVideo className="h-8 w-8 shrink-0 text-primary/70" />
												<div className="min-w-0 flex-1">
													<div className="truncate font-medium text-sm" title={stream.file_path}>
														{stream.file_path.split("/").pop()}
													</div>
													<div className="mt-1 flex items-center gap-2">
														<span className="font-bold text-success text-xs">STREAMING</span>
														<span className="text-base-content/40 text-xs">•</span>
														<span className="text-base-content/60 text-xs">
															{formatBytes(stream.total_size)}
														</span>
													</div>
												</div>
												<div className="shrink-0 text-right">
													<div className="flex flex-col items-end">
														<div className="flex items-center gap-1 font-bold text-info text-xs">
															<span className="text-[8px] text-base-content/80">IN:</span>
															{formatSpeed(stream.download_speed)}
															{stream.download_speed > 0 && stream.download_speed < 1024 * 1024 && (
																<div className="badge badge-warning badge-xs h-3 px-1 text-[8px]">
																	SLOW
																</div>
															)}
														</div>
														<div className="flex items-center gap-1 font-bold font-mono text-primary text-xs">
															<span className="text-[8px] text-base-content/80 text-success">OUT:</span>
															{formatSpeed(stream.bytes_per_second)}
														</div>
													</div>
													{stream.eta > 0 && (
														<div className="font-mono text-base-content/40 text-xs">
															{formatDuration(stream.eta)} left
														</div>
													)}
												</div>
											</div>

											<div className="mt-1 space-y-1">
												<div className="flex items-center justify-between px-0.5 text-xs">
													<div className="flex items-center gap-2">
														<span className="font-medium text-primary">{progress}%</span>
														<span className="text-base-content/40">•</span>
														<span
															className="text-base-content/40"
															title="Total downloaded from Usenet for this session"
														>
															DL: {formatBytes(stream.bytes_downloaded)}
														</span>
													</div>
													<span className="text-base-content/40">
														{formatBytes(position)} / {formatBytes(stream.total_size)}
													</span>
												</div>
												<div className="relative h-1.5 w-full overflow-hidden rounded-full bg-neutral">
													{bufferedProgress > progress && (
														<div
															className="absolute top-0 left-0 h-full bg-primary/20 transition-all duration-500 ease-out"
															style={{ width: `${bufferedProgress}%` }}
														/>
													)}
													<div
														className="absolute top-0 left-0 h-full bg-primary transition-all duration-500 ease-out"
														style={{ width: `${progress}%` }}
													/>
												</div>
											</div>
										</div>
									);
								})
							) : (
								<div className="py-10 text-center text-base-content/50">
									<Play className="mx-auto mb-2 h-8 w-8 opacity-20" />
									<p>No active streams</p>
								</div>
							)}
						</div>
					)}

					{activeTab === "imports" && (
						<div className="space-y-4">
							{queueLoading ? (
								<div className="flex justify-center py-10">
									<LoadingSpinner />
								</div>
							) : enrichedQueueItems.length > 0 ? (
								enrichedQueueItems.map((item) => {
									const progress = item.percentage ?? 0;

									return (
										<div
											key={item.id}
											className="group flex flex-col gap-2 rounded-lg bg-base-200/30 p-3"
										>
											<div className="flex items-center gap-3">
												<div className="relative">
													<Download
														className={`h-8 w-8 shrink-0 ${progress > 0 ? "text-secondary" : "text-base-content/20"}`}
													/>
													{progress > 0 && (
														<span className="-top-1 -right-1 absolute flex h-3 w-3">
															<span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-secondary opacity-75" />
															<span className="relative inline-flex h-3 w-3 rounded-full bg-secondary" />
														</span>
													)}
												</div>
												<div className="min-w-0 flex-1">
													<div className="truncate font-medium text-sm" title={item.nzb_path}>
														{item.target_path || item.nzb_path.split("/").pop()}
													</div>
													<div className="mt-1 flex items-center gap-2">
														<span className="font-bold text-secondary text-xs">IMPORTING</span>
														<span className="text-base-content/40 text-xs">•</span>
														<span className="text-base-content/60 text-xs">
															Worker #{item.id % 10}
														</span>
														{item.file_size && (
															<>
																<span className="text-base-content/40 text-xs">•</span>
																<span className="text-base-content/60 text-xs">
																	{formatBytes(item.file_size)}
																</span>
															</>
														)}
													</div>
												</div>
												<div className="shrink-0 text-right">
													<div className="font-bold text-secondary text-sm">{progress}%</div>
													<div className="text-base-content/40 text-xs">
														Attempt {item.retry_count + 1}
													</div>
												</div>
											</div>

											<div className="mt-1 space-y-1">
												<div className="relative h-1.5 w-full overflow-hidden rounded-full bg-neutral">
													<div
														className="absolute top-0 left-0 h-full bg-secondary transition-all duration-500 ease-out"
														style={{ width: `${progress}%` }}
													/>
												</div>
											</div>
										</div>
									);
								})
							) : (
								<div className="py-10 text-center text-base-content/50">
									<Download className="mx-auto mb-2 h-8 w-8 opacity-20" />
									<p>No active imports</p>
								</div>
							)}
						</div>
					)}

					{activeTab === "history" && (
						<div className="space-y-3">
							{historyLoading ? (
								<div className="flex justify-center py-10">
									<LoadingSpinner />
								</div>
							) : importHistory && importHistory.length > 0 ? (
								importHistory.map((item) => (
									<div
										key={item.id}
										className="flex items-center justify-between gap-4 rounded-lg border-success border-l-4 bg-base-200/30 p-2 text-sm"
									>
										<div className="flex min-w-0 items-center gap-3 truncate">
											<CheckCircle2 className="h-4 w-4 shrink-0 text-success" />
											<div className="flex flex-col truncate">
												<span className="truncate font-medium" title={item.file_name}>
													{item.file_name}
												</span>
												<span className="truncate text-base-content/50 text-xs">
													{item.category || "No Category"} • {formatBytes(item.file_size)}
												</span>
											</div>
										</div>
										<span className="shrink-0 whitespace-nowrap text-base-content/40 text-xs">
											{formatRelativeTime(item.completed_at)}
										</span>
									</div>
								))
							) : (
								<div className="py-10 text-center text-base-content/50">
									<History className="mx-auto mb-2 h-8 w-8 opacity-20" />
									<p>No import history</p>
								</div>
							)}
						</div>
					)}
				</div>
			</div>
		</div>
	);
}
