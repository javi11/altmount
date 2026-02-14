import { Activity, FileVideo, Globe, MonitorPlay, Network, User } from "lucide-react";
import { useActiveStreams } from "../../hooks/useApi";
import { formatBytes, formatDuration, truncateText } from "../../lib/utils";
import { LoadingSpinner } from "../ui/LoadingSpinner";

export function ActiveStreamsCard() {
	const { data: allStreams, isLoading, error } = useActiveStreams();

	// Filter to show only active streaming sessions (WebDAV or FUSE)
	const streams = allStreams?.filter(
		(s) => (s.source === "WebDAV" || s.source === "FUSE") && s.status === "Streaming",
	);

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
			<div className="card h-full bg-base-100 shadow-lg">
				<div className="card-body items-center justify-center">
					<LoadingSpinner />
				</div>
			</div>
		);
	}

	return (
		<div className="card h-full bg-base-100 shadow-lg">
			<div className="card-body p-4">
				<h2 className="card-title mb-4 flex items-center gap-2 font-medium text-base">
					<MonitorPlay className="h-5 w-5 text-primary" />
					Active Streams
					{streams && streams.length > 0 && (
						<div className="badge badge-primary badge-sm">{streams.length}</div>
					)}
				</h2>

				{!streams || streams.length === 0 ? (
					<div className="flex flex-col items-center justify-center py-8 text-base-content/50">
						<MonitorPlay className="mb-2 h-12 w-12 opacity-20" />
						<p className="text-sm">No active streams</p>
					</div>
				) : (
					<div className="space-y-3">
						{streams.map((stream) => {
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
									className="group flex flex-col gap-2 rounded-lg bg-base-200/50 p-3"
								>
									<div className="flex items-center gap-3">
										<div className="mt-1">
											<FileVideo className="h-8 w-8 text-primary/70" />
										</div>
										<div className="min-w-0 flex-1">
											<div className="truncate font-medium text-sm" title={stream.file_path}>
												{truncateText(stream.file_path.split("/").pop() || "", 40)}
											</div>

											{/* User / Client Info */}
											<div className="mt-1.5 flex flex-wrap items-center gap-2 text-[10px] text-base-content/60">
												{(stream.user_name || stream.client_ip) && (
													<div className="flex items-center gap-1 rounded bg-base-300/50 px-1.5 py-0.5">
														{stream.user_name ? (
															<User className="h-3 w-3" />
														) : (
															<Globe className="h-3 w-3" />
														)}
														<span
															className="max-w-[100px] truncate"
															title={stream.user_name || stream.client_ip}
														>
															{stream.user_name || stream.client_ip}
														</span>
													</div>
												)}

												{stream.user_agent && (
													<div
														className="flex items-center gap-1 rounded border border-base-content/10 px-1.5 py-0.5"
														title={stream.user_agent}
													>
														<span className="max-w-[80px] truncate">
															{stream.user_agent.split("/")[0]}
														</span>
													</div>
												)}

												{stream.total_connections > 1 && (
													<div className="flex items-center gap-1 font-mono text-primary/80">
														<Network className="h-3 w-3" />
														<span>{stream.total_connections}</span>
													</div>
												)}
											</div>

											<div className="mt-1.5 flex items-center gap-2 text-[10px]">
												{stream.bytes_per_second > 0 ? (
													<span className="animate-pulse font-bold text-success">STREAMING</span>
												) : (
													<span className="font-bold text-base-content/40">IDLE</span>
												)}
												<span className="text-base-content/40">•</span>
												<span className="text-base-content/60">
													{formatBytes(stream.total_size)}
												</span>
											</div>
										</div>
									</div>

									<div className="space-y-1">
										<div className="flex items-center justify-between px-0.5 text-[10px]">
											<div className="flex items-center gap-2">
												<span className="font-medium text-primary">{progress}%</span>
												<span className="text-base-content/40">•</span>
												<span
													className="text-base-content/40"
													title="Total downloaded for this session"
												>
													DL: {formatBytes(stream.bytes_downloaded)}
												</span>
											</div>
											<div className="flex flex-col items-end gap-1 font-mono opacity-80">
												{/* Speeds */}
												<div className="flex items-center gap-2">
													{/* Download (Input) Speed */}
													<div className="flex items-center gap-1 text-info">
														<span className="text-[8px] opacity-60">IN:</span>
														<span className="whitespace-nowrap font-bold">
															{formatBytes(stream.download_speed)}/s
														</span>
														{stream.download_speed > 0 && stream.download_speed < 1024 * 1024 && (
															<div className="badge badge-warning badge-xs h-3 px-1 text-[8px]">
																SLOW
															</div>
														)}
													</div>

													<span className="opacity-20">|</span>

													{/* Playback (Output) Speed */}
													<div className="flex items-center gap-1 text-success">
														<span className="text-[8px] opacity-60">OUT:</span>
														<span className="whitespace-nowrap font-bold">
															{formatBytes(stream.bytes_per_second)}/s
														</span>
													</div>
												</div>

												{/* ETA */}
												{stream.eta > 0 && (
													<span className="whitespace-nowrap text-[9px] opacity-60">
														ETA: {formatDuration(stream.eta)}
													</span>
												)}
											</div>
										</div>

										{/* Custom progress bar with buffer */}
										<div className="relative h-1.5 w-full overflow-hidden rounded-full bg-neutral">
											{/* Buffer Bar */}
											{bufferedProgress > progress && (
												<div
													className="absolute top-0 left-0 h-full bg-primary/20 transition-all duration-500 ease-out"
													style={{ width: `${bufferedProgress}%` }}
												/>
											)}
											{/* Playback Progress Bar */}
											<div
												className={`absolute top-0 left-0 h-full transition-all duration-500 ease-out ${
													stream.bytes_per_second > 0 ? "bg-primary" : "bg-base-content/20"
												}`}
												style={{ width: `${progress}%` }}
											/>
										</div>

										<div className="flex justify-end font-mono text-[9px] text-base-content/40">
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
