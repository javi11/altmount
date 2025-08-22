import { useCallback, useEffect, useRef } from "react";
import { FileExplorer } from "../components/files/FileExplorer";
import { useWebDAVConnection } from "../hooks/useWebDAV";

export function FilesPage() {
	const {
		isConnected,
		hasConnectionFailed,
		connect,
		isConnecting,
		connectionError,
	} = useWebDAVConnection();

	// Track connection attempts to prevent rapid retries
	const connectionAttempted = useRef(false);

	// Stable connect function to prevent useEffect loops
	const handleConnect = useCallback(() => {
		if (!connectionAttempted.current && !isConnected && !isConnecting) {
			connectionAttempted.current = true;
			connect();
		}
	}, [isConnected, isConnecting, connect]);

	// Auto-connect on page load using cookie authentication
	useEffect(() => {
		handleConnect();
	}, [handleConnect]);

	// Reset connection attempt flag when connection state changes
	useEffect(() => {
		if (isConnected || connectionError) {
			// Reset flag to allow retry on manual retry button
			connectionAttempted.current = false;
		}
	}, [isConnected, connectionError]);

	// Handle manual retry connection
	const handleRetryConnection = useCallback(() => {
		connectionAttempted.current = false; // Reset flag for manual retry
		connect();
	}, [connect]);

	return (
		<FileExplorer
			isConnected={isConnected}
			hasConnectionFailed={hasConnectionFailed}
			isConnecting={isConnecting}
			connectionError={connectionError}
			onRetryConnection={handleRetryConnection}
		/>
	);
}
