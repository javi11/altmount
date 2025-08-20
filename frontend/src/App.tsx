import { QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { Layout } from "./components/layout/Layout";
import { queryClient } from "./lib/queryClient";
import { Dashboard } from "./pages/Dashboard";
import { HealthPage } from "./pages/HealthPage";
import { QueuePage } from "./pages/QueuePage";
import { SystemPage } from "./pages/SystemPage";

function App() {
	return (
		<QueryClientProvider client={queryClient}>
			<BrowserRouter>
				<div className="min-h-screen bg-base-100" data-theme="light">
					<Routes>
						<Route path="/" element={<Layout />}>
							<Route index element={<Dashboard />} />
							<Route path="queue" element={<QueuePage />} />
							<Route path="health" element={<HealthPage />} />
							<Route path="system" element={<SystemPage />} />
						</Route>
					</Routes>
				</div>
			</BrowserRouter>
			<ReactQueryDevtools initialIsOpen={false} />
		</QueryClientProvider>
	);
}

export default App;
