package parser

import (
	"strings"
	"testing"

	"github.com/javi11/nzbparser"
	"github.com/stretchr/testify/assert"
)

func TestNzbParserNamespaces(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{
			name:      "2003 Namespace",
			namespace: "http://www.newzbin.com/DTD/2003/nzb",
		},
		{
			name:      "1.1 Namespace",
			namespace: "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd",
		},
		{
			name:      "No Namespace",
			namespace: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nzbXML := `<?xml version="1.0" encoding="UTF-8"?>`
			if tt.namespace != "" {
				nzbXML += `<nzb xmlns="` + tt.namespace + `">`
			} else {
				nzbXML += `<nzb>`
			}
			nzbXML += `
 <file poster="poster" date="123456789" subject="test file">
  <groups>
   <group>alt.binaries.test</group>
  </groups>
  <segments>
   <segment bytes="100" number="1">seg1</segment>
  </segments>
 </file>
</nzb>`
			r := strings.NewReader(nzbXML)
			n, err := nzbparser.Parse(r)
			assert.NoError(t, err)
			assert.Equal(t, 1, len(n.Files), "Should find 1 file with namespace %s", tt.namespace)
		})
	}
}
