package fileinfo

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// Video file extensions (common video formats)
	videoExtensions = map[string]bool{
		".webm": true, ".m4v": true, ".3gp": true, ".nsv": true, ".ty": true, ".strm": true,
		".rm": true, ".rmvb": true, ".m3u": true, ".ifo": true, ".mov": true, ".qt": true,
		".divx": true, ".xvid": true, ".bivx": true, ".nrg": true, ".pva": true, ".wmv": true,
		".asf": true, ".asx": true, ".ogm": true, ".ogv": true, ".m2v": true, ".avi": true,
		".bin": true, ".dat": true, ".dvr-ms": true, ".mpg": true, ".mpeg": true, ".mp4": true,
		".avc": true, ".vp3": true, ".svq3": true, ".nuv": true, ".viv": true, ".dv": true,
		".fli": true, ".flv": true, ".wpl": true, ".img": true, ".iso": true, ".vob": true,
		".mkv": true, ".mk3d": true, ".ts": true, ".wtv": true, ".m2ts": true,
	}

	// RAR file pattern: .rar or .r00, .r01, etc.
	rarPattern = regexp.MustCompile(`(?i)\.r(ar|\d+)$|\.part\d+\.rar$`)

	// 7z file pattern: .7z or .7z.001, .7z.002, etc.
	sevenZipPattern = regexp.MustCompile(`(?i)\.7z(\.(\d+))?$`)

	// Multipart MKV pattern: .mkv.001, .mkv.002, etc.
	multipartMkvPattern = regexp.MustCompile(`(?i)\.mkv\.(\d+)$`)
)

// HasRar4Magic checks if the data contains RAR 4.x magic bytes
func HasRar4Magic(data []byte) bool {
	return len(data) >= len(Rar4Magic) && bytes.Equal(data[:len(Rar4Magic)], Rar4Magic)
}

// HasRar5Magic checks if the data contains RAR 5.x magic bytes
func HasRar5Magic(data []byte) bool {
	return len(data) >= len(Rar5Magic) && bytes.Equal(data[:len(Rar5Magic)], Rar5Magic)
}

// HasRarMagic checks if the data contains any RAR magic bytes (4.x or 5.x)
func HasRarMagic(data []byte) bool {
	return HasRar4Magic(data) || HasRar5Magic(data)
}

// IsVideoFile checks if the filename is a video file based on extension
func IsVideoFile(filename string) bool {
	if filename == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(filename))
	return videoExtensions[ext]
}

// IsRarFile checks if the filename is a RAR file based on extension pattern
func IsRarFile(filename string) bool {
	if filename == "" {
		return false
	}
	return rarPattern.MatchString(filename)
}

// Is7zFile checks if the filename is a 7z file based on extension pattern
func Is7zFile(filename string) bool {
	if filename == "" {
		return false
	}

	return sevenZipPattern.MatchString(filename)
}

// IsMultipartMkv checks if the filename is a multipart MKV file
func IsMultipartMkv(filename string) bool {
	if filename == "" {
		return false
	}
	return multipartMkvPattern.MatchString(filename)
}

// IsImportantFileType checks if the filename is an important file type
// (video, RAR, 7z, or multipart MKV)
func IsImportantFileType(filename string) bool {
	return IsVideoFile(filename) ||
		IsRarFile(filename) ||
		Is7zFile(filename) ||
		IsMultipartMkv(filename)
}

// HasValidExtensionLength checks if the extension length is between 2 and 4 characters
// (considered a valid/common extension length)
func HasValidExtensionLength(filename string) bool {
	if filename == "" {
		return false
	}
	ext := filepath.Ext(filename)
	if ext == "" {
		return false
	}
	// Remove the leading dot
	extLen := len(ext) - 1
	return extLen >= 2 && extLen <= 4
}
