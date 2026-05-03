# Provider Health Enhancements Plan

## Objective
Implement three enhancements to the Provider Health feature:
1. **Manual Quota Reset UI:** Add a "Reset Quota" button to the `ProviderQuota` component.
2. **Actionable Alerts for Maxed Quotas:** Display a warning banner when quotas are exceeded.
3. **Speed History Trends:** Track and plot top speeds historically.

## Implementation Steps

### Enhancement 1: Manual Quota Reset UI
- Update `frontend/src/hooks/useApi.ts` to export `useResetProviderQuota`.
- Update `ProviderQuota.tsx` to include a "Reset Quota" button next to providers that have an active quota.
- Add a confirmation modal or simple confirmation state before calling the reset API.

### Enhancement 2: Actionable Alerts for Maxed Quotas
- Update `ProviderHealth.tsx` to compute if any provider has reached 100% quota.
- If so, render an `AlertTriangle` warning banner at the top of the page.
- Include a quick-action "Reset All Maxed Quotas" button or instructions within the banner.

### Enhancement 4: Speed History Trends
- **Database Backend:** Create a new table `provider_speed_tests_history` with columns `(id, provider_id, speed_mbps, created_at)`.
- Create a migration file in `internal/database/migrations/sqlite` and `postgres`.
- **Repository:** Add `RecordSpeedTest(providerID, speedMbps)` and `GetProviderSpeedTestHistory(days)`.
- **API Handler:** Modify `handleTestProviderSpeed` and `handleTestImportProviderSpeed` in `internal/api/provider_speedtest_handler.go` (or wherever they are) to log the speed test result to the history table.
- **Frontend Hook:** Add `useProviderSpeedHistory(days)`.
- **UI:** Add a `ProviderSpeedChart.tsx` component (similar to `ProviderChart.tsx`) to plot the top speeds over time and display it in `ProviderHealth.tsx`.
