# Implementation Plan - UI/UX Refactor and Queue Bug Fixes

## Phase 1: Layout & Sidebar Optimization
Goal: Reduce sidebar width to increase content area to ~80%.

- [ ] Task: Research sidebar width implementation in `frontend/src/components/layout/Sidebar.tsx`.
- [ ] Task: Adjust sidebar width and content container layout.
    - [ ] Write failing test for Sidebar width/class properties.
    - [ ] Implement reduced width using Tailwind responsive classes.
    - [ ] Adjust main layout wrapper to ensure content expands to filled space.
- [ ] Task: Conductor - User Manual Verification 'Layout & Sidebar Optimization' (Protocol in workflow.md)

## Phase 2: Visual Separation & Contrast
Goal: Enhance block contrast and add distinct borders.

- [ ] Task: Identify global or shared "Card" components used for UI blocks.
- [ ] Task: Enhance card contrast and borders.
    - [ ] Write tests for Card component styling props (if applicable).
    - [ ] Add distinct borders (e.g., `border border-base-300` or similar) to UI blocks.
    - [ ] Adjust background colors of sections to provide clearer separation from the main black background.
- [ ] Task: Conductor - User Manual Verification 'Visual Separation & Contrast' (Protocol in workflow.md)

## Phase 3: Functionality Bug Fix (Queue Menu)
Goal: Fix the "three dots" menu not opening in the Failed Queue.

- [ ] Task: Reproduce the failure in `frontend/src/components/queue/`.
- [ ] Task: Fix the context menu rendering/event logic.
    - [ ] Write failing test simulating a click on the "three dots" menu in the Failed state.
    - [ ] Investigate `Dropdown` or `Menu` components in the Queue table.
    - [ ] Implement fix for the event handler or conditional rendering logic.
- [ ] Task: Conductor - User Manual Verification 'Functionality Bug Fix (Queue Menu)' (Protocol in workflow.md)

## Phase 4: Final Polish & Verification
Goal: Ensure consistency across all pages.

- [ ] Task: Perform a sweep of all pages (Dashboard, Queue, Health, Files, Config) to ensure layout consistency.
- [ ] Task: Verify responsive behavior on mobile and tablet views.
- [ ] Task: Conductor - User Manual Verification 'Final Polish & Verification' (Protocol in workflow.md)
