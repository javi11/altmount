import { useEffect, useRef, useState } from "react";
import type { LogEntry } from "../api/client";

const MAX_ENTRIES = 500;

interface LogsStreamData {
	type: "initial" | "update";
	data: LogEntry[] | LogEntry;
}

interface UseLogsStreamReturn {
	entries: LogEntry[];
	isConnected: boolean;
	error: Error | null;
}

/**
 * Hook for consuming Server-Sent Events (SSE) log updates.
 * Provides real-time log streaming from the backend log file.
 */
export function useLogsStream(): UseLogsStreamReturn {
	const [entries, setEntries] = useState<LogEntry[]>([]);
	const [isConnected, setIsConnected] = useState(false);
	const [error, setError] = useState<Error | null>(null);
	const eventSourceRef = useRef<EventSource | null>(null);
	const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
	const reconnectAttemptsRef = useRef<number>(0);
	const maxReconnectAttempts = 10;

	useEffect(() => {
		const connect = () => {
			if (eventSourceRef.current) {
				eventSourceRef.current.close();
			}

			try {
				const eventSource = new EventSource("/api/logs/stream");
				eventSourceRef.current = eventSource;

				eventSource.onopen = () => {
					setIsConnected(true);
					setError(null);
					reconnectAttemptsRef.current = 0;
				};

				eventSource.onmessage = (event) => {
					setIsConnected(true);
					try {
						const data: LogsStreamData = JSON.parse(event.data);

						if (data.type === "initial") {
							setEntries(data.data as LogEntry[]);
						} else if (data.type === "update") {
							const entry = data.data as LogEntry;
							setEntries((prev) => {
								const next = [entry, ...prev];
								return next.length > MAX_ENTRIES ? next.slice(0, MAX_ENTRIES) : next;
							});
						}
					} catch (err) {
						console.error("Failed to parse log stream event:", err);
					}
				};

				eventSource.onerror = () => {
					setIsConnected(false);
					setError(new Error("Connection lost"));
					eventSource.close();

					if (reconnectAttemptsRef.current < maxReconnectAttempts) {
						const backoffTime = Math.min(1000 * 2 ** reconnectAttemptsRef.current, 30000);
						reconnectAttemptsRef.current += 1;
						reconnectTimeoutRef.current = setTimeout(connect, backoffTime);
					} else {
						setError(new Error("Failed to reconnect after multiple attempts"));
					}
				};
			} catch (err) {
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
	}, []);

	return { entries, isConnected, error };
}
