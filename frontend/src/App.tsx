import { QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { ProtectedRoute, UserManagement } from "./components/auth";
import { Layout } from "./components/layout/Layout";
import { AuthProvider } from "./contexts/AuthContext";
import { queryClient } from "./lib/queryClient";
import { Dashboard } from "./pages/Dashboard";
import { FilesPage } from "./pages/FilesPage";
import { HealthPage } from "./pages/HealthPage";
import { QueuePage } from "./pages/QueuePage";
import { ConfigurationPage } from "./pages/ConfigurationPage";

function App() {
	return (
		<QueryClientProvider client={queryClient}>
			<AuthProvider>
				<BrowserRouter>
					<div className="min-h-screen bg-base-100" data-theme="light">
						<Routes>
							{/* Protected routes */}
							<Route
								path="/"
								element={
									<ProtectedRoute>
										<Layout />
									</ProtectedRoute>
								}
							>
								<Route index element={<Dashboard />} />
								<Route path="queue" element={<QueuePage />} />
								<Route path="health" element={<HealthPage />} />
								<Route path="files" element={<FilesPage />} />

								{/* Admin-only routes */}
								<Route
									path="admin"
									element={
										<ProtectedRoute requireAdmin>
											<UserManagement />
										</ProtectedRoute>
									}
								/>
								<Route
									path="config"
									element={
										<ProtectedRoute requireAdmin>
											<ConfigurationPage />
										</ProtectedRoute>
									}
								/>
							</Route>
						</Routes>
					</div>
				</BrowserRouter>
			</AuthProvider>
			<ReactQueryDevtools initialIsOpen={false} />
		</QueryClientProvider>
	);
}

export default App;
