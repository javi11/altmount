import { useEffect, useRef, useState } from "react";

export interface ProgressEntry {
	percentage: number;
	stage?: string;
}

interface ProgressUpdate {
	queue_id: number;
	percentage: number;
	stage?: string;
	status?: string;
	timestamp: string;
}

interface ProgressData {
	type: "initial" | "update";
	data: Record<number, ProgressEntry> | ProgressUpdate;
}

interface UseProgressStreamReturn {
	progress: Record<number, ProgressEntry>;
	isConnected: boolean;
	error: Error | null;
}

interface UseProgressStreamOptions {
	enabled?: boolean;
}

/**
 * Hook for consuming Server-Sent Events (SSE) progress updates
 * Provides real-time progress tracking for queue items
 * @param options.enabled - Whether to enable the SSE connection (default: true)
 */
export function useProgressStream(options: UseProgressStreamOptions = {}): UseProgressStreamReturn {
	const { enabled = true } = options;
	const [progress, setProgress] = useState<Record<number, ProgressEntry>>({});
	const [isConnected, setIsConnected] = useState(false);
	const [error, setError] = useState<Error | null>(null);
	const eventSourceRef = useRef<EventSource | null>(null);
	const reconnectTimeoutRef = useRef<NodeJS.Timeout | undefined>(undefined);
	const reconnectAttemptsRef = useRef<number>(0);
	const maxReconnectAttempts = 10;

	useEffect(() => {
		// Don't connect if disabled
		if (!enabled) {
			// Close existing connection if enabled was toggled off
			if (eventSourceRef.current) {
				eventSourceRef.current.close();
				eventSourceRef.current = null;
			}
			if (reconnectTimeoutRef.current) {
				clearTimeout(reconnectTimeoutRef.current);
				reconnectTimeoutRef.current = undefined;
			}
			setIsConnected(false);
			setError(null);
			return;
		}
		const connect = () => {
			// Close existing connection
			if (eventSourceRef.current) {
				eventSourceRef.current.close();
			}

			try {
				const eventSource = new EventSource("/api/queue/stream");
				eventSourceRef.current = eventSource;

				eventSource.onopen = () => {
					console.log("Progress stream connected");
					setIsConnected(true);
					setError(null);
					reconnectAttemptsRef.current = 0; // Reset reconnect counter on successful connection
				};

				eventSource.onmessage = (event) => {
					try {
						const data: ProgressData = JSON.parse(event.data);

						if (data.type === "initial") {
							// Initial state: replace all progress
							setProgress(data.data as Record<number, ProgressEntry>);
						} else if (data.type === "update") {
							// Incremental update: merge with existing state
							const update = data.data as ProgressUpdate;

							// Skip non-progress sentinel events
							if (
								update.status === "queue_changed" ||
								update.status === "completed" ||
								update.status === "failed" ||
								update.status === "health_changed"
							) {
								return;
							}

							setProgress((prev) => {
								const newProgress = { ...prev };

								// If percentage is 100 or greater, remove from tracking
								if (update.percentage >= 100) {
									delete newProgress[update.queue_id];
								} else {
									newProgress[update.queue_id] = {
										percentage: update.percentage,
										stage: update.stage,
									};
								}

								return newProgress;
							});
						}
					} catch (err) {
						console.error("Failed to parse progress update:", err);
					}
				};

				eventSource.onerror = (err) => {
					console.error("Progress stream error:", err);
					setIsConnected(false);
					setError(new Error("Connection lost"));
					eventSource.close();

					// Implement exponential backoff for reconnection
					if (reconnectAttemptsRef.current < maxReconnectAttempts) {
						const backoffTime = Math.min(1000 * 2 ** reconnectAttemptsRef.current, 30000); // Max 30s
						reconnectAttemptsRef.current += 1;

						console.log(
							`Reconnecting to progress stream (attempt ${reconnectAttemptsRef.current}/${maxReconnectAttempts}) in ${backoffTime}ms...`,
						);

						reconnectTimeoutRef.current = setTimeout(() => {
							connect();
						}, backoffTime);
					} else {
						console.error("Max reconnection attempts reached. Giving up.");
						setError(new Error("Failed to reconnect after multiple attempts"));
					}
				};
			} catch (err) {
				console.error("Failed to create EventSource:", err);
				setError(err instanceof Error ? err : new Error("Unknown error"));
				setIsConnected(false);
			}
		};

		connect();

		return () => {
			if (eventSourceRef.current) {
				eventSourceRef.current.close();
			}
			if (reconnectTimeoutRef.current) {
				clearTimeout(reconnectTimeoutRef.current);
			}
		};
	}, [enabled]);

	return { progress, isConnected, error };
}
