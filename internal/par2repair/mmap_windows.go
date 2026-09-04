//go:build windows

package par2repair

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// mmapFile maps size bytes of f read-write and shared via a file mapping
// object. The mapping handle is closed right away: the view keeps the mapping
// alive until UnmapViewOfFile.
func mmapFile(f *os.File, size int) ([]byte, error) {
	h, err := windows.CreateFileMapping(windows.Handle(f.Fd()), nil, windows.PAGE_READWRITE,
		uint32(uint64(size)>>32), uint32(uint64(size)&0xFFFFFFFF), nil)
	if err != nil {
		return nil, err
	}
	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_WRITE, 0, 0, uintptr(size))
	_ = windows.CloseHandle(h)
	if err != nil {
		return nil, err
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(addr)), size), nil
}

func munmapFile(data []byte) error {
	return windows.UnmapViewOfFile(uintptr(unsafe.Pointer(&data[0])))
}
