import { QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { lazy, Suspense } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { ProtectedRoute } from "./components/auth/ProtectedRoute";
import { Layout } from "./components/layout/Layout";
import { ErrorBoundary } from "./components/ui/ErrorBoundary";
import { PWAUpdatePrompt } from "./components/ui/PWAUpdatePrompt";
import { ToastContainer } from "./components/ui/ToastContainer";
import { AuthProvider } from "./contexts/AuthContext";
import { ModalProvider } from "./contexts/ModalContext";
import { ToastProvider } from "./contexts/ToastContext";
import { queryClient } from "./lib/queryClient";

// Pages are code-split so the initial bundle does not eagerly include every
// route (the largest pages are 600-1800 LOC). React.lazy needs a default
// export, so each named page export is adapted inline.
const Dashboard = lazy(() => import("./pages/Dashboard").then((m) => ({ default: m.Dashboard })));
const QueuePage = lazy(() => import("./pages/QueuePage").then((m) => ({ default: m.QueuePage })));
const HealthPage = lazy(() =>
	import("./pages/HealthPage").then((m) => ({ default: m.HealthPage })),
);
const FilesPage = lazy(() => import("./pages/FilesPage").then((m) => ({ default: m.FilesPage })));
const LogsPage = lazy(() => import("./pages/LogsPage").then((m) => ({ default: m.LogsPage })));
const ConfigurationPage = lazy(() =>
	import("./pages/ConfigurationPage").then((m) => ({ default: m.ConfigurationPage })),
);

function PageFallback() {
	return (
		<div className="flex min-h-[400px] items-center justify-center">
			<span className="loading loading-spinner loading-lg" />
			<span className="sr-only">Loading page</span>
		</div>
	);
}

function App() {
	return (
		<QueryClientProvider client={queryClient}>
			<ToastProvider>
				<ModalProvider>
					<AuthProvider>
						<BrowserRouter>
							<div className="min-h-screen bg-base-100">
								<ErrorBoundary>
									<Suspense fallback={<PageFallback />}>
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
												<Route path="logs" element={<LogsPage />} />

												{/* Admin-only routes */}
												<Route
													path="config"
													element={
														<ProtectedRoute requireAdmin>
															<ConfigurationPage />
														</ProtectedRoute>
													}
												/>
												<Route
													path="config/:section"
													element={
														<ProtectedRoute requireAdmin>
															<ConfigurationPage />
														</ProtectedRoute>
													}
												/>
											</Route>
										</Routes>
									</Suspense>
								</ErrorBoundary>
							</div>
							<ToastContainer />
							<PWAUpdatePrompt />
						</BrowserRouter>
					</AuthProvider>
				</ModalProvider>
			</ToastProvider>
			<ReactQueryDevtools initialIsOpen={false} />
		</QueryClientProvider>
	);
}

export default App;
