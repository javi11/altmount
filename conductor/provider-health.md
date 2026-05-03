# Provider Health Redesign Plan

## Objective
Completely redesign the Provider Health feature in the frontend to include historical data usage (daily, weekly, monthly, yearly, custom) and a clear visualization of provider quotas, backed by a new historical stats API.

## Key Files & Context
- **Backend:**
  - `internal/api/system_handlers.go`: Needs a new handler `/api/system/provider-stats`.
  - `internal/api/server.go`: Register the new endpoint.
  - `internal/database/repository.go`: Needs a new function `GetProviderStats(ctx, start, end, interval)` to query `provider_hourly_stats`.
  - `internal/pool/manager.go` / `internal/api/types.go`: Ensure quota information is properly exposed (already present, but verify types).
- **Frontend:**
  - `frontend/src/pages/HealthPage/components/ProviderHealth/ProviderHealth.tsx`: Needs to be completely redesigned to include charts and quota bars.
  - `frontend/src/hooks/useApi.ts`: Add new API hook `useProviderStats` to fetch historical stats.
  - `frontend/src/types/api.ts`: Add types for the new stats response.

## Implementation Steps

### Phase 1: Backend API
1. **Database Query**: Add `GetProviderHistoricalStats(ctx context.Context, days int, interval string)` to the repository. The query will aggregate `bytes_downloaded` from `provider_hourly_stats` grouped by the specified interval (e.g., day, week, month) or simply return daily points that the frontend can aggregate.
2. **API Endpoint**: Create `handleGetProviderStats` in `internal/api/system_handlers.go` bound to `GET /api/system/provider-stats`.
   - Query params: `days` (number, e.g., 1, 7, 30, 365), `interval` (string, e.g., "daily", "weekly").
3. **Registration**: Add the route in `internal/api/server.go`.

### Phase 2: Frontend API Integration
1. **Types**: Add `ProviderHistoricalStats` to `frontend/src/types/api.ts`.
2. **Hooks**: Add `useProviderHistoricalStats(days, interval)` to `frontend/src/hooks/useApi.ts` using `@tanstack/react-query`.

### Phase 3: Frontend UI Redesign
1. **Data Usage Chart Section**:
   - Add a dropdown for Time Range: "Last 24 Hours", "Last 7 Days", "Last 30 Days", "This Year", "Custom (Days)".
   - Implement an interactive area/bar chart using `recharts` to display data usage over time.
2. **Quota Section**:
   - For each provider with a configured quota, display a progress bar showing `QuotaUsed` vs `QuotaBytes`.
   - Calculate and display the reset date (`QuotaResetAt`).
3. **Refactor Existing Metrics**:
   - Keep the global metrics cards (Download Traffic, Articles, Active Connections).
   - Reorganize the layout so it is clean and intuitive.

## Verification & Testing
- **Backend Test**: Write unit tests for the new database query in `internal/database/repository_test.go` and the handler.
- **Frontend Verification**: Ensure the charts render correctly with different time ranges and quota progress bars accurately reflect the limits and reset dates.
