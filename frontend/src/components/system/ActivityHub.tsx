import { Download, Play, FileVideo, History, CheckCircle2 } from "lucide-react";
import { useState, useMemo } from "react";
import { useActiveStreams, useQueue, useImportHistory } from "../../hooks/useApi";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import { StatusBadge } from "../ui/StatusBadge";
import { formatBytes, formatSpeed, formatDuration, formatRelativeTime } from "../../lib/utils";
import type { ActiveStream } from "../../types/api";

export function ActivityHub() {
	const [activeTab, setActiveTab] = useState<"playback" | "imports" | "history">("playback");
	const { data: allStreams, isLoading: streamsLoading } = useActiveStreams();
	const { data: queueItems, isLoading: queueLoading } = useQueue({ status: "processing", limit: 10 });
	const { data: importHistory, isLoading: historyLoading } = useImportHistory(20, activeTab === "history" ? 10000 : 60000);

	// Group streams by file_path to show "unique playback sessions"
	const groupedStreams = useMemo(() => {
		if (!allStreams) return [];
		
		// Filter to show only active streaming sessions (WebDAV or FUSE)
		const streamingOnly = allStreams.filter(
			(s) => {
				const isSystemSource = (s.source === "WebDAV" || s.source === "FUSE");
				const isStreaming = s.status === "Streaming";
				
				// Heuristic: Filter out metadata probes and very short system scans
				// 1. If reading the very end of the file (last 10MB), it's likely a probe
				const isAtEnd = s.total_size > 0 && s.current_offset > (s.total_size - (10 * 1024 * 1024));
				// 2. If it's very new and hasn't sent much data yet, hide it temporarily 
				// (Plex/Infuse will pass this threshold in seconds)
				const isTooNew = s.bytes_sent < (20 * 1024 * 1024);
				const ageSeconds = (new Date().getTime() - new Date(s.started_at).getTime()) / 1000;
				
				if (isAtEnd) return false;
				if (isTooNew && ageSeconds < 15) return false;
				
				return isSystemSource && isStreaming;
			}
		);

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
	const importCount = queueItems?.data?.length || 0;

	return (
		<div className="card bg-base-100 shadow-lg min-h-[400px]">
			<div className="card-body p-0">
				<div className="tabs tabs-bordered w-full grid grid-cols-3">
					<button
						type="button"
						className={`tab tab-lg gap-2 ${activeTab === "playback" ? "tab-active font-bold border-primary text-primary" : ""}`}
						onClick={() => setActiveTab("playback")}
					>
						<Play className="h-4 w-4" />
						Playback
						{playbackCount > 0 && <span className="badge badge-sm badge-primary">{playbackCount}</span>}
					</button>
					<button
						type="button"
						className={`tab tab-lg gap-2 ${activeTab === "imports" ? "tab-active font-bold border-secondary text-secondary" : ""}`}
						onClick={() => setActiveTab("imports")}
					>
						<Download className="h-4 w-4" />
						Imports
						{importCount > 0 && <span className="badge badge-sm badge-secondary">{importCount}</span>}
					</button>
					<button
						type="button"
						className={`tab tab-lg gap-2 ${activeTab === "history" ? "tab-active font-bold border-accent text-accent" : ""}`}
						onClick={() => setActiveTab("history")}
					>
						<History className="h-4 w-4" />
						History
					</button>
				</div>

				<div className="p-4 overflow-y-auto max-h-[350px]">
					{activeTab === "playback" && (
						<div className="space-y-4">
							{streamsLoading ? (
								<div className="flex justify-center py-10"><LoadingSpinner /></div>
							) : groupedStreams.length > 0 ? (
								groupedStreams.map((stream) => {
									const position = stream.current_offset > 0 ? stream.current_offset : stream.bytes_sent;
									const progress = stream.total_size > 0 ? Math.round((position / stream.total_size) * 100) : 0;
									const bufferedProgress = stream.total_size > 0 ? Math.round((stream.buffered_offset / stream.total_size) * 100) : 0;

									return (
										<div key={stream.id} className="group flex flex-col gap-2 rounded-lg bg-base-200/30 p-3">
											<div className="flex items-center gap-3">
												<FileVideo className="h-8 w-8 text-primary/70 shrink-0" />
												<div className="min-w-0 flex-1">
													<div className="truncate font-medium text-sm" title={stream.file_path}>
														{stream.file_path.split("/").pop()}
													</div>
													<div className="flex items-center gap-2 mt-1">
														<span className="text-[10px] text-success font-bold">STREAMING</span>
														<span className="text-base-content/40 text-[10px]">•</span>
														<span className="text-base-content/60 text-[10px]">{formatBytes(stream.total_size)}</span>
													</div>
												</div>
												<div className="text-right shrink-0">
													<div className="flex flex-col items-end">
														{stream.download_speed > 0 && (
															<div className="text-[10px] text-info font-bold flex items-center gap-1">
																<span className="opacity-60 text-[8px]">IN:</span>
																{formatSpeed(stream.download_speed)}
															</div>
														)}
														<div className="text-xs font-mono text-primary font-bold flex items-center gap-1">
															<span className="opacity-60 text-[8px] text-success">OUT:</span>
															{formatSpeed(stream.bytes_per_second)}
														</div>
													</div>
													{stream.eta > 0 && (
														<div className="text-[10px] text-base-content/40 font-mono">
															{formatDuration(stream.eta)} left
														</div>
													)}
												</div>
											</div>

											<div className="space-y-1 mt-1">
												<div className="flex justify-between items-center px-0.5 text-[10px]">
													<div className="flex items-center gap-2">
														<span className="font-medium text-primary">{progress}%</span>
														<span className="text-base-content/40">•</span>
														<span className="text-base-content/40" title="Total downloaded from Usenet for this session">
															DL: {formatBytes(stream.bytes_downloaded)}
														</span>
													</div>
													<span className="text-base-content/40">{formatBytes(position)} / {formatBytes(stream.total_size)}</span>
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
								<div className="text-center py-10 text-base-content/50">
									<Play className="h-8 w-8 mx-auto mb-2 opacity-20" />
									<p>No active streams</p>
								</div>
							)}
						</div>
					)}

					{activeTab === "imports" && (
						<div className="space-y-4">
							{queueLoading ? (
								<div className="flex justify-center py-10"><LoadingSpinner /></div>
							) : queueItems?.data && queueItems.data.length > 0 ? (
								queueItems.data.map((item) => (
									<div key={item.id} className="group flex flex-col gap-2 rounded-lg bg-base-200/30 p-3 border-l-4 border-secondary">
										<div className="flex justify-between items-start">
											<div className="min-w-0 flex-1">
												<div className="truncate font-medium text-sm" title={item.nzb_path}>
													{item.target_path || item.nzb_path.split('/').pop()}
												</div>
												<div className="flex items-center gap-2 mt-1 text-[10px] text-base-content/60">
													<StatusBadge status="processing" className="badge-xs h-4" />
													<span>Worker #{item.id % 10}</span>
													<span>•</span>
													<span>Attempt {item.retry_count + 1}</span>
												</div>
											</div>
											{item.percentage !== undefined && (
												<div className="text-xs font-bold text-secondary">{item.percentage}%</div>
											)}
										</div>
										
										<div className="mt-1">
											{item.percentage !== undefined ? (
												<progress 
													className="progress progress-secondary w-full h-1.5" 
													value={item.percentage} 
													max="100"
												/>
											) : (
												<progress className="progress progress-secondary w-full h-1.5 animate-pulse" />
											)}
										</div>
									</div>
								))
							) : (
								<div className="text-center py-10 text-base-content/50">
									<Download className="h-8 w-8 mx-auto mb-2 opacity-20" />
									<p>No active imports</p>
								</div>
							)}
						</div>
					)}

					{activeTab === "history" && (
						<div className="space-y-3">
							{historyLoading ? (
								<div className="flex justify-center py-10"><LoadingSpinner /></div>
							) : importHistory && importHistory.length > 0 ? (
								importHistory.map((item) => (
									<div key={item.id} className="flex items-center justify-between gap-4 rounded-lg bg-base-200/30 p-2 text-sm border-l-4 border-success">
										<div className="flex items-center gap-3 truncate min-w-0">
											<CheckCircle2 className="h-4 w-4 text-success shrink-0" />
											<div className="truncate flex flex-col">
												<span className="truncate font-medium" title={item.file_name}>
													{item.file_name}
												</span>
												<span className="text-[10px] text-base-content/50 truncate">
													{item.category || "No Category"} • {formatBytes(item.file_size)}
												</span>
											</div>
										</div>
										<span className="text-[10px] text-base-content/40 whitespace-nowrap shrink-0">
											{formatRelativeTime(item.completed_at)}
										</span>
									</div>
								))
							) : (
								<div className="text-center py-10 text-base-content/50">
									<History className="h-8 w-8 mx-auto mb-2 opacity-20" />
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
