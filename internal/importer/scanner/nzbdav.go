package scanner

import (
	"context"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/nzbdav"
)

// BatchQueueAdder defines the interface for batch queue operations
type BatchQueueAdder interface {
	AddBatchToQueue(ctx context.Context, items []*database.ImportQueueItem) error
}

// NzbDavImporter handles importing from NZBDav databases
type NzbDavImporter struct {
	impl *nzbdav.Importer
}

// NewNzbDavImporter creates a new NZBDav importer
func NewNzbDavImporter(batchAdder BatchQueueAdder) *NzbDavImporter {
	return &NzbDavImporter{
		impl: nzbdav.NewImporter(batchAdder),
	}
}

// Start starts an asynchronous import from an NZBDav database
func (n *NzbDavImporter) Start(dbPath string, rootFolder string, nzbDir string, cleanupFile bool) error {
	return n.impl.Start(dbPath, rootFolder, nzbDir, cleanupFile)
}

// GetStatus returns the current import status
func (n *NzbDavImporter) GetStatus() ImportInfo {
	status := n.impl.GetStatus()
	
	return ImportInfo{
		Status:    convertStatus(status.Status),
		Total:     status.Total,
		Added:     status.Added,
		Failed:    status.Failed,
		LastError: status.LastError,
	}
}

// Cancel cancels the current import operation
func (n *NzbDavImporter) Cancel() error {
	return n.impl.Cancel()
}

func convertStatus(s nzbdav.ImportStatus) ImportJobStatus {
	switch s {
	case nzbdav.StatusIdle:
		return ImportStatusIdle
	case nzbdav.StatusRunning:
		return ImportStatusRunning
	case nzbdav.StatusCanceling:
		return ImportStatusCanceling
	default:
		return ImportStatusIdle
	}
}