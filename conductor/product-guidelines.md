# Product Guidelines - AltMount

## Tone and Voice
- **Technical & Concise:** Documentation and UI labels should be direct and technically accurate without unnecessary fluff.
- **Helpful & Proactive:** Error messages should not only state the problem but also suggest a potential fix or link to relevant documentation.
- **Consistent:** Use standard terminology throughout the application (e.g., always use "Mount" instead of "Drive" or "Volume").

## Design & UX Principles
- **Clarity Over Complexity:** The dashboard should prioritize the most important metrics (active streams, provider health, queue status) and hide advanced configurations behind progressive disclosure.
- **Visual Feedback:** Every user action (starting a scan, updating config) should provide immediate visual feedback via toasts or progress indicators.
- **Responsiveness:** The web interface must be fully functional on both desktop and mobile devices, utilizing Tailwind's responsive utilities.
- **Dark Mode First:** Given the target audience (Home Server Admins), the UI should prioritize a clean dark mode aesthetic while remaining accessible.

## Documentation Standards
- **Markdown Primary:** All user-facing documentation should be written in Markdown and hosted via the Docusaurus site.
- **In-Code Documentation:**
  - **Go:** All exported functions, types, and constants must have GoDoc comments.
  - **TypeScript:** Use strong typing and interface definitions. Complicated logic should be explained with concise JSDoc comments.
- **API Documentation:** The REST API should follow standard HTTP conventions and be documented with clear request/response examples.

## Technical Quality
- **Performance First:** Critical paths (streaming, FUSE operations) must be optimized for low latency and high throughput.
- **Robust Error Handling:** The system should handle network timeouts and NNTP provider failures gracefully, with automated retry logic.
- **Data Integrity:** Never compromise on segment verification. If a segment is missing or corrupted, the user must be informed or a repair must be triggered.
