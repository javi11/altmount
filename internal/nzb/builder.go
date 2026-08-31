package nzb

import (
	"bytes"
	"encoding/xml"
	"strconv"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

const nzbHeader = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE nzb PUBLIC \"-//newzBin//DTD NZB 1.1//EN\" \"http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd\">\n"

// BuildNZB renders an NzbStore as a valid NZB 1.1 XML document.
func BuildNZB(store *metapb.NzbStore) []byte {
	var b bytes.Buffer
	b.WriteString(nzbHeader)
	b.WriteString("<nzb xmlns=\"http://www.newzbin.com/DTD/2003/nzb\">\n")
	for _, f := range store.Files {
		b.WriteString("  <file poster=\"")
		xml.EscapeText(&b, []byte(f.Poster)) //nolint:errcheck
		b.WriteString("\" date=\"" + strconv.FormatInt(f.Date, 10) + "\" subject=\"")
		xml.EscapeText(&b, []byte(f.Subject)) //nolint:errcheck
		b.WriteString("\">\n    <groups>\n")
		for _, g := range f.Groups {
			b.WriteString("      <group>")
			xml.EscapeText(&b, []byte(g)) //nolint:errcheck
			b.WriteString("</group>\n")
		}
		b.WriteString("    </groups>\n    <segments>\n")
		for _, s := range f.Segments {
			b.WriteString("      <segment bytes=\"" + strconv.FormatInt(s.Bytes, 10) +
				"\" number=\"" + strconv.Itoa(int(s.Number)) + "\">")
			xml.EscapeText(&b, []byte(s.Id)) //nolint:errcheck
			b.WriteString("</segment>\n")
		}
		b.WriteString("    </segments>\n  </file>\n")
	}
	b.WriteString("</nzb>\n")
	return b.Bytes()
}
