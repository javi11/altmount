# Track Specification: UI/UX Refactor and Queue Bug Fixes

## 1. Overview
This track addresses significant user feedback regarding a recent UI regression. Users have reported that the sidebar menu is too wide, the content area is too small, and the visual contrast between UI blocks is insufficient. Additionally, a functional bug in the "Failed Queue" context menu needs resolution.

## 2. Problem Statement
- **Layout:** The sidebar menu occupies ~40% of the screen, leaving only ~60% for content, making the UI feel cramped and "hidden."
- **Contrast:** UI blocks (cards/sections) lack clear separation due to a "black on slightly lighter black" color scheme without distinct borders.
- **Functionality:** The "three dots" context menu in the "Failed Queue" section does not open when clicked.

## 3. Goals
- **Maximize Content Area:** Reduce sidebar width to restore ~80% of the screen space for main content.
- **Improve Visual Hierarchy:** Use distinct borders and contrast to make UI blocks perceived as separate sections.
- **Fix Regressions:** Ensure context menus in the Queue section are fully functional.

## 4. Functional Requirements
- **Sidebar Refactor:**
    - Adjust the CSS/Tailwind classes for the Sidebar component to reduce its fixed or percentage width.
    - Ensure the layout remains responsive.
- **UI Block Enhancement:**
    - Update the styling for "Card" or "Block" components to include distinct borders (e.g., border-1 with a medium gray or primary color).
    - Potentially adjust the background color of cards slightly to provide better separation from the main page background.
- **Queue Menu Fix:**
    - Investigate and fix the event handler or rendering logic for the context menu in `frontend/src/components/queue/`.

## 5. Non-Functional Requirements
- **Design Consistency:** Stay within the established DaisyUI / Tailwind CSS theme.
- **Performance:** Ensure layout adjustments don't cause layout shift or performance degradation.

## 6. Acceptance Criteria
- [ ] Sidebar width is reduced, and content area takes up significantly more space (target ~80% on desktop).
- [ ] Individual sections/cards are clearly distinguishable from the background and each other via borders or contrast.
- [ ] Clicking the "three dots" icon on a failed item in the queue opens the action menu.

## 7. Out of Scope
- Changing the primary "Black" background of the application.
- Adding new navigation items or features.
