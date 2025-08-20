# File Health Management System

This package provides a comprehensive file health management system for the usenet filesystem, combining metadata updates with database tracking for efficient monitoring and recovery.

## Components

### 1. Database Schema (`002_file_health_schema.sql`)

Creates the `file_health` table to track corrupted files with retry logic:
- Exponential backoff retry strategy  
- Status tracking (healthy/partial/corrupted)
- Source NZB file reference
- JSON error details

### 2. FileHealth Model (`database/models.go`)

Go struct representing health records with full lifecycle tracking.

### 3. HealthRepository (`database/health_repository.go`)

Database operations for health management:
- `UpdateFileHealth()` - Update/insert health records
- `GetUnhealthyFiles()` - Get files needing retry
- `IncrementRetryCount()` - Exponential backoff logic
- `MarkAsCorrupted()` - Permanently mark after max retries
- `GetHealthStats()` - Health statistics

### 4. Automatic Health Updates (`metadata_remote_file.go`)

Modified `MetadataVirtualFile.Read()` to automatically:
- Update metadata status (FILE_STATUS_PARTIAL/CORRUPTED)  
- Track errors in database (non-blocking)
- Provide rich error context

### 5. HealthChecker Service (`health/checker.go`)

Background service for monitoring and recovery:
- Periodic health checks with configurable interval
- Retry logic with exponential backoff
- Event system for external integration  
- Concurrent file verification
- Configurable segment checking (first segment vs all segments)
- Connection pooling for segment verification

## Usage Example

```go
package main

import (
    "context"
    "database/sql"
    "time"
    
    "github.com/javi11/altmount/internal/database"
    "github.com/javi11/altmount/internal/health"
    "github.com/javi11/altmount/internal/metadata"
    "github.com/javi11/altmount/internal/adapters/nzbfilesystem"
)

func main() {
    // Initialize components
    db, _ := sql.Open("sqlite3", "./health.db")
    metadataService := metadata.NewMetadataService("./metadata")
    healthRepo := database.NewHealthRepository(db)
    
    // Create filesystem with health tracking
    remoteFile := nzbfilesystem.NewMetadataRemoteFile(
        metadataService,
        healthRepo, 
        usenetPool,
        10,
        nzbfilesystem.MetadataRemoteFileConfig{},
    )
    
    // Configure health checker
    config := health.HealthCheckerConfig{
        CheckInterval:         30 * time.Minute,
        MaxConcurrentJobs:     3,
        BatchSize:             10,
        MaxSegmentConnections: 5,     // Max concurrent connections for segment checking
        CheckAllSegments:      true,  // Check all segments (false = first segment only)
        EventHandler:          handleHealthEvents,
    }
    
    checker := health.NewHealthChecker(
        healthRepo,
        metadataService, 
        usenetPool,
        config,
    )
    
    // Start background monitoring
    ctx := context.Background()
    checker.Start(ctx)
    
    // Files are automatically tracked when read operations fail
    // Health checker runs periodic recovery attempts
}

func handleHealthEvents(event health.HealthEvent) {
    switch event.Type {
    case health.EventTypeFileRecovered:
        log.Printf("File recovered: %s", event.FilePath)
        // Notify external systems about recovery
        
    case health.EventTypeFileCorrupted:  
        log.Printf("File corrupted: %s", event.FilePath)
        // Trigger external repair/download systems
        
    case health.EventTypeCheckFailed:
        log.Printf("Health check failed: %s - %v", event.FilePath, event.Error)
    }
}
```

## Configuration Options

### HealthCheckerConfig Parameters

- **`CheckInterval`**: How often to run health check cycles (default: 30 minutes)
- **`MaxConcurrentJobs`**: Maximum concurrent file health checks (default: 3)  
- **`BatchSize`**: Number of files to check per cycle (default: 10)
- **`MaxRetries`**: Maximum retry attempts before marking permanently corrupted (default: 5)
- **`MaxSegmentConnections`**: Maximum concurrent NNTP connections for segment checking (default: 5)
- **`CheckAllSegments`**: Whether to check all segments vs just the first one (default: false)
  - `false`: Only check first segment (faster, good for quick health overview)
  - `true`: Check all segments (thorough, detects partial corruption accurately)
- **`EventHandler`**: Optional callback for health events

### Performance Considerations

**Fast Mode (CheckAllSegments=false)**:
- Only checks first segment per file
- ~100x faster for files with many segments
- Good for detecting completely missing files
- May miss partial corruption

**Thorough Mode (CheckAllSegments=true)**:
- Checks every segment in the file
- Accurately detects partial corruption  
- Uses `MaxSegmentConnections` to limit NNTP load
- Recommended for critical files or comprehensive audits

## Benefits

1. **Fast Monitoring**: Database queries vs filesystem traversal
2. **Files Stay Accessible**: Corrupted files remain in place for potential recovery  
3. **Rich Retry Logic**: Exponential backoff with configurable limits
4. **Event Integration**: Clean hooks for external repair systems
5. **Non-blocking Updates**: Health updates don't slow down file operations
6. **Comprehensive Tracking**: Full error context and retry history

## Architecture Decisions

### Why Hybrid Approach?

- **Database for Performance**: Fast bulk queries for monitoring
- **Metadata for Truth**: File status reflects real-time health
- **Files in Place**: Better for caching and user transparency
- **Background Recovery**: Non-blocking retry logic

### Why Not Move Files?

- Moving files breaks caching layers
- Users lose visibility into file status  
- Complex file management across directories
- Database queries are already fast enough

### Why Not Database Only?

- Metadata is the source of truth for file operations
- Dual updates ensure consistency
- Better integration with existing filesystem code