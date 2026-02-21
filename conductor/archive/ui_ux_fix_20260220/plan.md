# Implementation Plan - UI/UX Refactor and Queue Bug Fixes

## Phase 1: Layout & Sidebar Optimization
Goal: Reduce sidebar width to increase content area to ~80%.

- [x] Task: Research sidebar width implementation in `frontend/src/components/layout/Sidebar.tsx`.
- [x] Task: Adjust sidebar width and content container layout.
    - [x] Write failing test for Sidebar width/class properties.
    - [x] Implement reduced width using Tailwind responsive classes.
    - [x] Adjust main layout wrapper to ensure content expands to filled space.
- [x] Task: Conductor - User Manual Verification 'Layout & Sidebar Optimization' (Protocol in workflow.md)

## Phase 2: Visual Separation & Contrast
Goal: Enhance block contrast and add distinct borders.

- [x] Task: Identify global or shared "Card" components used for UI blocks.
- [x] Task: Enhance card contrast and borders.
    - [x] Write tests for Card component styling props (if applicable).
    - [x] Add distinct borders (e.g., `border border-base-300` or similar) to UI blocks.
    - [x] Adjust background colors of sections to provide clearer separation from the main black background.
- [x] Task: Conductor - User Manual Verification 'Visual Separation & Contrast' (Protocol in workflow.md)

## Phase 3: Functionality Bug Fix (Queue Menu)
Goal: Fix the "three dots" menu not opening in the Failed Queue.

- [x] Task: Reproduce the failure in `frontend/src/components/queue/`.
- [x] Task: Fix the context menu rendering/event logic.
    - [x] Write failing test simulating a click on the "three dots" menu in the Failed state.
    - [x] Investigate `Dropdown` or `Menu` components in the Queue table.
    - [x] Implement fix for the event handler or conditional rendering logic.
- [x] Task: Conductor - User Manual Verification 'Functionality Bug Fix (Queue Menu)' (Protocol in workflow.md)

## Phase 4: Final Polish & Verification
Goal: Ensure consistency across all pages.

- [x] Task: Perform a sweep of all pages (Dashboard, Queue, Health, Files, Config) to ensure layout consistency.
- [x] Task: Verify responsive behavior on mobile and tablet views.
- [x] Task: Conductor - User Manual Verification 'Final Polish & Verification' (Protocol in workflow.md)

## Phase: Review Fixes
- [x] Task: Apply review suggestions 530b489
