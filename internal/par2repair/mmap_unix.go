//go:build unix

package par2repair

import (
	"os"

	"golang.org/x/sys/unix"
)

// mmapFile maps size bytes of f read-write and shared, so dirty pages flush
// to the backing file under memory pressure instead of occupying heap.
func mmapFile(f *os.File, size int) ([]byte, error) {
	return unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
}

func munmapFile(data []byte) error {
	return unix.Munmap(data)
}
