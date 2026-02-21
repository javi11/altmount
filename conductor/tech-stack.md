# Tech Stack - AltMount

## Backend
- **Programming Language:** Go (1.26.0)
- **Web Framework:** Fiber (v2.52.9)
- **Database:** SQLite (mattn/go-sqlite3)
- **Filesystem Integration:** Gofuse (v2.9.0)
- **Encryption/Security:** Go-crypto, EME (for rclone-compatible filename encryption)
- **Configuration Management:** Viper (v1.20.0), Cobra (CLI support)
- **Asynchronous Processing:** Go-sync, Conc (for structured concurrency)
- **Logging:** Slog (standard library) with Lumberjack for log rotation

## Frontend
- **Framework:** React (v19.1.1)
- **Tooling:** Vite (v7.1.2)
- **Language:** TypeScript (~v5.8.3)
- **Styling:** Tailwind CSS (v4.1.12), DaisyUI (v5.0.50)
- **State Management:** TanStack React Query (v5.85.5)
- **Form Management:** React Hook Form (v7.62.0)
- **Routing:** React Router DOM (v7.8.1)
- **Data Visualization:** Recharts (v3.1.2)
- **Linting & Formatting:** Biome (v2.2.2)

## Infrastructure & DevOps
- **Containerization:** Docker with s6-overlay for process management
- **Deployment:** GitHub Actions (CI/CD for testing and building dev images)
- **Documentation:** Docusaurus (TypeScript-based static site generator)

## Key Libraries & Tools
- **NNTP Pooling:** nntppool (v4.3.0)
- **NZB Parsing:** nzbparser (v0.5.4)
- **Archive Extraction:** rardecode/v2 (RAR), sevenzip (7z)
- **WebDAV:** webdav-server (backend), webdav client (frontend)
- **Starr API Support:** starr (v1.2.0) for Sonarr/Radarr integration
