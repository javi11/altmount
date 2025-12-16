import { Activity, FileVideo, MonitorPlay, Square } from "lucide-react";
import { formatDistanceToNowStrict } from "date-fns";
import { useActiveStreams, useDeleteActiveStream } from "../../hooks/useApi";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import { truncateText } from "../../lib/utils";

export function ActiveStreamsCard() {
	const { data: allStreams, isLoading, error } = useActiveStreams();
	const { mutate: stopStream } = useDeleteActiveStream();

	// Filter to show only WebDAV streams (covers RClone and external players)
	const streams = allStreams?.filter((s) => s.source === "WebDAV");

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
						{streams.map((stream) => (
							<div key={stream.id} className="flex items-center gap-3 p-3 bg-base-200/50 rounded-lg group">
								<div className="mt-1">
									<FileVideo className="h-8 w-8 text-primary/70" />
								</div>
								<div className="flex-1 min-w-0">
									<div className="font-medium text-sm truncate" title={stream.file_path}>
										{truncateText(stream.file_path.split("/").pop() || "", 40)}
									</div>
									<div className="text-xs text-base-content/60 flex flex-col gap-0.5 mt-1">
										<div className="flex justify-between">
											<span>{stream.user_name || "Unknown User"}</span>
											<span>
												{formatDistanceToNowStrict(new Date(stream.started_at), { addSuffix: true })}
											</span>
										</div>
									</div>
								</div>
								<button
									className="btn btn-ghost btn-xs text-error opacity-0 group-hover:opacity-100 transition-opacity"
									onClick={() => stopStream(stream.id)}
									title="Stop Stream"
								>
									<Square className="h-4 w-4 fill-current" />
								</button>
							</div>
						))}
					</div>
				)}
			</div>
		</div>
	);
}
