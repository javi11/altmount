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

interface QueueStreamData {
	type: "initial" | "update";
	data: Record<number, ProgressEntry> | ProgressUpdate;
}

interface UseQueueStreamReturn {
	progress: Record<number, ProgressEntry>;
	isConnected: boolean;
	error: Error | null;
}

interface UseQueueStreamOptions {
	enabled?: boolean;
	onQueueChanged?: () => void;
}

/**
 * Hook for consuming Server-Sent Events (SSE) queue stream updates.
 * Provides real-time progress tracking and queue-change notifications.
 * @param options.enabled - Whether to enable the SSE connection (default: true)
 * @param options.onQueueChanged - Callback fired when the queue list changes
 */
export function useQueueStream(options: UseQueueStreamOptions = {}): UseQueueStreamReturn {
	const { enabled = true, onQueueChanged } = options;
	const [progress, setProgress] = useState<Record<number, ProgressEntry>>({});
	const [isConnected, setIsConnected] = useState(false);
	const [error, setError] = useState<Error | null>(null);
	const eventSourceRef = useRef<EventSource | null>(null);
	const reconnectTimeoutRef = useRef<NodeJS.Timeout | undefined>(undefined);
	const reconnectAttemptsRef = useRef<number>(0);
	const onQueueChangedRef = useRef(onQueueChanged);
	onQueueChangedRef.current = onQueueChanged;
	const maxReconnectAttempts = 10;

	useEffect(() => {
		if (!enabled) {
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
			if (eventSourceRef.current) {
				eventSourceRef.current.close();
			}

			try {
				const eventSource = new EventSource("/api/queue/stream");
				eventSourceRef.current = eventSource;

				eventSource.onopen = () => {
					setIsConnected(true);
					setError(null);
					reconnectAttemptsRef.current = 0;
				};

				eventSource.onmessage = (event) => {
					try {
						const data: QueueStreamData = JSON.parse(event.data);

						if (data.type === "initial") {
							setProgress(data.data as Record<number, ProgressEntry>);
							onQueueChangedRef.current?.();
						} else if (data.type === "update") {
							const update = data.data as ProgressUpdate;

							if (update.status === "queue_changed") {
								onQueueChangedRef.current?.();
								return;
							}

							if (update.status === "completed" || update.status === "failed") {
								onQueueChangedRef.current?.();
								setProgress((prev) => {
									const next = { ...prev };
									delete next[update.queue_id];
									return next;
								});
								return;
							}

							setProgress((prev) => {
								const next = { ...prev };
								if (update.percentage >= 100) {
									delete next[update.queue_id];
								} else {
									next[update.queue_id] = {
										percentage: update.percentage,
										stage: update.stage,
									};
								}
								return next;
							});
						}
					} catch (err) {
						console.error("Failed to parse queue stream update:", err);
					}
				};

				eventSource.onerror = (err) => {
					console.error("Queue stream error:", err);
					setIsConnected(false);
					setError(new Error("Connection lost"));
					eventSource.close();

					if (reconnectAttemptsRef.current < maxReconnectAttempts) {
						const backoffTime = Math.min(1000 * 2 ** reconnectAttemptsRef.current, 30000);
						reconnectAttemptsRef.current += 1;
						reconnectTimeoutRef.current = setTimeout(() => {
							connect();
						}, backoffTime);
					} else {
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
