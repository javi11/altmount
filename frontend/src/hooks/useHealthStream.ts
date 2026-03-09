import { useEffect, useRef, useState } from "react";

interface UseHealthStreamOptions {
	enabled?: boolean;
	onHealthChanged?: () => void;
}

interface UseHealthStreamReturn {
	isConnected: boolean;
	error: Error | null;
}

/**
 * Hook for consuming Server-Sent Events (SSE) health stream updates.
 * Fires onHealthChanged on initial connection and on every health state change.
 * @param options.enabled - Whether to enable the SSE connection (default: true)
 * @param options.onHealthChanged - Callback fired when health state changes
 */
export function useHealthStream(options: UseHealthStreamOptions = {}): UseHealthStreamReturn {
	const { enabled = true, onHealthChanged } = options;
	const [isConnected, setIsConnected] = useState(false);
	const [error, setError] = useState<Error | null>(null);
	const eventSourceRef = useRef<EventSource | null>(null);
	const reconnectTimeoutRef = useRef<NodeJS.Timeout | undefined>(undefined);
	const reconnectAttemptsRef = useRef<number>(0);
	const onHealthChangedRef = useRef(onHealthChanged);
	onHealthChangedRef.current = onHealthChanged;
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
				const eventSource = new EventSource("/api/health/stream");
				eventSourceRef.current = eventSource;

				eventSource.onopen = () => {
					setIsConnected(true);
					setError(null);
					reconnectAttemptsRef.current = 0;
				};

				eventSource.onmessage = (event) => {
					try {
						const data = JSON.parse(event.data) as { type: "initial" | "update" };
						if (data.type === "initial" || data.type === "update") {
							onHealthChangedRef.current?.();
						}
					} catch (err) {
						console.error("Failed to parse health stream update:", err);
					}
				};

				eventSource.onerror = () => {
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

	return { isConnected, error };
}
