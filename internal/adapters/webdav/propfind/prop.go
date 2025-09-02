package propfind

// Code copied from webdav package of golang.org/x/net/webdav to override

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"golang.org/x/net/webdav"
)

// makePropstats returns a slice containing those of x and y whose Props slice
// is non-empty. If both are empty, it returns a slice containing an otherwise
// zero webdav.Propstat whose HTTP status code is 200 OK.
func makePropstats(x, y webdav.Propstat) []webdav.Propstat {
	pstats := make([]webdav.Propstat, 0, 2)
	if len(x.Props) != 0 {
		pstats = append(pstats, x)
	}
	if len(y.Props) != 0 {
		pstats = append(pstats, y)
	}
	if len(pstats) == 0 {
		pstats = append(pstats, webdav.Propstat{
			Status: http.StatusOK,
		})
	}
	return pstats
}

// liveProps contains all supported, protected DAV: properties.
var liveProps = map[xml.Name]struct {
	// findFn implements the propfind function of this webdav.Property. If nil,
	// it indicates a hidden webdav.Property.
	findFn func(context.Context, string, os.FileInfo) (string, error)
	// dir is true if the webdav.Property applies to directories.
	dir bool
}{
	{Space: "DAV:", Local: "resourcetype"}: {
		findFn: findResourceType,
		dir:    true,
	},
	{Space: "DAV:", Local: "displayname"}: {
		findFn: findDisplayName,
		dir:    true,
	},
	{Space: "DAV:", Local: "getcontentlength"}: {
		findFn: findContentLength,
		dir:    false,
	},
	{Space: "DAV:", Local: "getlastmodified"}: {
		findFn: findLastModified,
		// http://webdav.org/specs/rfc4918.html#webdav.Property_getlastmodified
		// suggests that getlastmodified should only apply to GETable
		// resources, and this package does not support GET on directories.
		//
		// Nonetheless, some WebDAV clients expect child directories to be
		// sortable by getlastmodified date, so this value is true, not false.
		// See golang.org/issue/15334.
		dir: true,
	},
	{Space: "DAV:", Local: "creationdate"}: {
		findFn: nil,
		dir:    false,
	},
	{Space: "DAV:", Local: "getcontentlanguage"}: {
		findFn: nil,
		dir:    false,
	},
	{Space: "DAV:", Local: "getcontenttype"}: {
		findFn: findContentType,
		dir:    false,
	},
	{Space: "DAV:", Local: "getetag"}: {
		findFn: findETag,
		// findETag implements ETag as the concatenated hex values of a file's
		// modification time and size. This is not a reliable synchronization
		// mechanism for directories, so we do not advertise getetag for DAV
		// collections.
		dir: false,
	},

	// TODO: The lockdiscovery webdav.Property requires webdav.LockSystem to list the
	// active locks on a resource.
	{Space: "DAV:", Local: "lockdiscovery"}: {},
	{Space: "DAV:", Local: "supportedlock"}: {
		findFn: findSupportedLock,
		dir:    true,
	},
	// Custom property to help clients identify same filesystem for MOVE operations
	{Space: "altmount:", Local: "filesystem-id"}: {
		findFn: findFilesystemId,
		dir:    true,
	},
}

// props returns the status of the properties named pnames for resource name.
//
// Each webdav.Propstat has a unique status and each webdav.Property name will only be part
// of one webdav.Propstat element.
func props(ctx context.Context, fi os.FileInfo, name string, pnames []xml.Name) ([]webdav.Propstat, error) {
	isDir := fi.IsDir()

	var deadProps map[xml.Name]webdav.Property
	pstatOK := webdav.Propstat{Status: http.StatusOK}
	pstatNotFound := webdav.Propstat{Status: http.StatusNotFound}
	for _, pn := range pnames {
		// If this file has dead properties, check if they contain pn.
		if dp, ok := deadProps[pn]; ok {
			pstatOK.Props = append(pstatOK.Props, dp)
			continue
		}
		// Otherwise, it must either be a live webdav.Property or we don't know it.
		if prop := liveProps[pn]; prop.findFn != nil && (prop.dir || !isDir) {
			innerXML, err := prop.findFn(ctx, name, fi)
			if err != nil {
				return nil, err
			}
			pstatOK.Props = append(pstatOK.Props, webdav.Property{
				XMLName:  pn,
				InnerXML: []byte(innerXML),
			})
		} else {
			pstatNotFound.Props = append(pstatNotFound.Props, webdav.Property{
				XMLName: pn,
			})
		}
	}
	return makePropstats(pstatOK, pstatNotFound), nil
}

// propnames returns the webdav.Property names defined for resource name.
func propnames(fi os.FileInfo) ([]xml.Name, error) {
	isDir := fi.IsDir()

	pnames := make([]xml.Name, 0, len(liveProps))
	for pn, prop := range liveProps {
		if prop.findFn != nil && (prop.dir || !isDir) {
			pnames = append(pnames, pn)
		}
	}

	return pnames, nil
}

// allprop returns the properties defined for resource name and the properties
// named in include.
//
// Note that RFC 4918 defines 'allprop' to return the DAV: properties defined
// within the RFC plus dead properties. Other live properties should only be
// returned if they are named in 'include'.
//
// See http://www.webdav.org/specs/rfc4918.html#METHOD_PROPFIND
func allprop(ctx context.Context, info os.FileInfo, name string, include []xml.Name) ([]webdav.Propstat, error) {
	pnames, err := propnames(info)
	if err != nil {
		return nil, err
	}
	// Add names from include if they are not already covered in pnames.
	nameset := make(map[xml.Name]bool)
	for _, pn := range pnames {
		nameset[pn] = true
	}
	for _, pn := range include {
		if !nameset[pn] {
			pnames = append(pnames, pn)
		}
	}
	return props(ctx, info, name, pnames)
}

func escapeXML(s string) string {
	for i := 0; i < len(s); i++ {
		// As an optimization, if s contains only ASCII letters, digits or a
		// few special characters, the escaped value is s itself and we don't
		// need to allocate a buffer and convert between string and []byte.
		switch c := s[i]; {
		case c == ' ' || c == '_' ||
			('+' <= c && c <= '9') || // Digits as well as + , - . and /
			('A' <= c && c <= 'Z') ||
			('a' <= c && c <= 'z'):
			continue
		}
		// Otherwise, go through the full escaping process.
		var buf bytes.Buffer
		_ = xml.EscapeText(&buf, []byte(s))
		return buf.String()
	}
	return s
}

func findResourceType(ctx context.Context, name string, fi os.FileInfo) (string, error) {
	if fi.IsDir() {
		return `<D:collection xmlns:D="DAV:"/>`, nil
	}
	return "", nil
}

func findDisplayName(ctx context.Context, name string, fi os.FileInfo) (string, error) {
	if slashClean(name) == "/" {
		// Hide the real name of a possibly prefixed root directory.
		return "", nil
	}
	return escapeXML(fi.Name()), nil
}

func findContentLength(ctx context.Context, name string, fi os.FileInfo) (string, error) {
	return strconv.FormatInt(fi.Size(), 10), nil
}

func findLastModified(ctx context.Context, name string, fi os.FileInfo) (string, error) {
	return fi.ModTime().UTC().Format(http.TimeFormat), nil
}

func findContentType(ctx context.Context, name string, fi os.FileInfo) (string, error) {
	// This implementation is based on serveContent's code in the standard net/http package.
	ctype := mime.TypeByExtension(filepath.Ext(name))
	if ctype != "" {
		return ctype, nil
	}

	return "application/octet-stream", nil
}

func findETag(ctx context.Context, name string, fi os.FileInfo) (string, error) {
	if do, ok := fi.(webdav.ETager); ok {
		etag, err := do.ETag(ctx)
		if err != webdav.ErrNotImplemented {
			return etag, err
		}
	}
	// The Apache http 2.4 web server by default concatenates the
	// modification time and size of a file. We replicate the heuristic
	// with nanosecond granularity.
	return fmt.Sprintf(`"%x%x"`, fi.ModTime().UnixNano(), fi.Size()), nil
}

func findSupportedLock(ctx context.Context, name string, fi os.FileInfo) (string, error) {
	return `` +
		`<D:lockentry xmlns:D="DAV:">` +
		`<D:lockscope><D:exclusive/></D:lockscope>` +
		`<D:locktype><D:write/></D:locktype>` +
		`</D:lockentry>`, nil
}

// findFilesystemId returns a unique identifier for the filesystem
// to help WebDAV clients identify when source and destination are on the same filesystem
func findFilesystemId(ctx context.Context, name string, fi os.FileInfo) (string, error) {
	// Return a static filesystem ID that's unique to this altmount instance
	// This helps clients like Sonarr/Radarr understand that MOVE operations 
	// should be used instead of COPY for files within the same mount
	return "altmount-nzbfs-v1", nil
}

// slashClean is equivalent to but slightly more efficient than
// path.Clean("/" + name).
func slashClean(name string) string {
	if name == "" || name[0] != '/' {
		name = "/" + name
	}
	return path.Clean(name)
}
