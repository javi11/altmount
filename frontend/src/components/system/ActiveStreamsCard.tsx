import { Activity, FileVideo, MonitorPlay } from "lucide-react";
import { useActiveStreams } from "../../hooks/useApi";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import { truncateText, formatBytes, formatDuration } from "../../lib/utils";

export function ActiveStreamsCard() {
	const { data: allStreams, isLoading, error } = useActiveStreams();

	// Filter to show only WebDAV or FUSE streams (covers RClone, FUSE and external players)
	const streams = allStreams?.filter((s) => s.source === "WebDAV" || s.source === "FUSE");

	if (error) {
		return (
			<div className="alert alert-error">
				<Activity className="h-6 w-6" />
				<span>Failed to load active streams</span>
			</div>
		);
	}

	if (isLoading) {
		return (
			<div className="card bg-base-100 shadow-lg h-full">
				<div className="card-body items-center justify-center">
					<LoadingSpinner />
				</div>
			</div>
		);
	}

	return (
		<div className="card bg-base-100 shadow-lg h-full">
			<div className="card-body p-4">
				<h2 className="card-title text-base font-medium flex items-center gap-2 mb-4">
					<MonitorPlay className="h-5 w-5 text-primary" />
					Active Streams
					{streams && streams.length > 0 && (
						<div className="badge badge-primary badge-sm">{streams.length}</div>
					)}
				</h2>

				{!streams || streams.length === 0 ? (
					<div className="flex flex-col items-center justify-center py-8 text-base-content/50">
						<MonitorPlay className="h-12 w-12 mb-2 opacity-20" />
						<p className="text-sm">No active streams</p>
					</div>
				) : (
					<div className="space-y-3">
						{streams.map((stream) => {
							const position = stream.current_offset > 0 ? stream.current_offset : stream.bytes_sent;
							const progress = stream.total_size > 0 
								? Math.round((position / stream.total_size) * 100) 
								: 0;

							return (
								<div key={stream.id} className="flex flex-col gap-2 p-3 bg-base-200/50 rounded-lg group">
									<div className="flex items-center gap-3">
										<div className="mt-1">
											<FileVideo className="h-8 w-8 text-primary/70" />
										</div>
										<div className="flex-1 min-w-0">
											<div className="font-medium text-sm truncate" title={stream.file_path}>
												{truncateText(stream.file_path.split("/").pop() || "", 40)}
											</div>
											<div className="text-[10px] flex items-center gap-2 mt-1">
												{stream.bytes_per_second > 0 ? (
													<span className="text-success font-bold animate-pulse">STREAMING</span>
												) : (
													<span className="text-base-content/40 font-bold">IDLE</span>
												)}
												<span className="text-base-content/40">•</span>
												<span className="text-base-content/60">{formatBytes(stream.total_size)}</span>
											</div>
										</div>
									</div>
									
									<div className="space-y-1">
										<div className="flex justify-between items-center text-[10px] px-0.5">
											<span className="font-medium text-primary">{progress}%</span>
											<div className="flex gap-2 opacity-70 font-mono">
												{stream.eta > 0 && (
													<span>ETA: {formatDuration(stream.eta)}</span>
												)}
												{stream.bytes_per_second > 0 && (
													<span>{formatBytes(stream.bytes_per_second)}/s</span>
												)}
											</div>
										</div>
										<progress 
											className={`progress ${stream.bytes_per_second > 0 ? 'progress-primary' : 'progress-neutral'} w-full h-1.5`} 
											value={position} 
											max={stream.total_size}
										></progress>
										<div className="flex justify-end text-[9px] text-base-content/40 font-mono">
											Avg: {formatBytes(stream.speed_avg)}/s
										</div>
									</div>
								</div>
							);
						})}
					</div>
				)}
			</div>
		</div>
	);
}
