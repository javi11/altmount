### Summary
This PR modernizes the AltMount Configuration UI with a cleaner aesthetic and significantly improved usability. It also introduces real-time NNTP latency tracking to help users identify the most responsive providers at a glance.

### Key Changes

#### 1. Configuration UI Overhaul
- **Modernized Design:** Implemented a consistent, card-based layout across all configuration sections (ARRs, Health, Providers, etc.).
- **Enhanced Usability:** Improved information hierarchy, added clearer validation error messages, and integrated loading indicators for all save operations.
- **Improved Responsiveness:** Overhauled all configuration forms to be fully responsive for mobile and desktop viewports.

#### 2. NNTP Latency Tracking (RTT)
- **Real-time Measurement:** The system now measures and displays the Round-Trip Time (RTT) for each NNTP provider during connection tests.
- **Persistence:** Latency results are persisted in the configuration, allowing users to see historical connectivity performance on the provider cards.
- **Improved Provider Cards:** Replaced technical "Password" indicators with clear **PRIMARY** / **BACKUP** role badges and immediate latency feedback.

#### 3. Optimized Configuration Handling
- **Batch Saving:** Enabled batched updates for provider-specific settings (Enabled toggle, Max Connections, Pipeline) to reduce unnecessary server restarts and API calls.
- **Partial Patching:** Enhanced the backend API to support clean, partial section updates for the providers list.

### Verification Results
- Manually verified all configuration forms save correctly and display validation errors.
- Confirmed latency data is accurately captured and persisted after successful connection tests.
- Validated UI stability during provider reordering and batch-saving operations.
