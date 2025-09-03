package propfind

// Code copied from webdav package of golang.org/x/net/webdav to override

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	ixml "github.com/javi11/altmount/internal/webdav/propfind/xml"
	"golang.org/x/net/webdav"
)

type countingReader struct {
	n int
	r io.Reader
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// next returns the next token, if any, in the XML stream of d.
// RFC 4918 requires to ignore comments, processing instructions
// and directives.
// http://www.webdav.org/specs/rfc4918.html#property_values
// http://www.webdav.org/specs/rfc4918.html#xml-extensibility
func next(d *ixml.Decoder) (ixml.Token, error) {
	for {
		t, err := d.Token()
		if err != nil {
			return t, err
		}
		switch t.(type) {
		case ixml.Comment, ixml.Directive, ixml.ProcInst:
			continue
		default:
			return t, nil
		}
	}
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_prop (for propfind)
type propfindProps []xml.Name

// UnmarshalXML appends the property names enclosed within start to pn.
//
// It returns an error if start does not contain any properties or if
// properties contain values. Character data between properties is ignored.
func (pn *propfindProps) UnmarshalXML(d *ixml.Decoder, start ixml.StartElement) error {
	for {
		t, err := next(d)
		if err != nil {
			return err
		}
		switch t.(type) {
		case ixml.EndElement:
			if len(*pn) == 0 {
				return fmt.Errorf("%s must not be empty", start.Name.Local)
			}
			return nil
		case ixml.StartElement:
			name := t.(ixml.StartElement).Name
			t, err = next(d)
			if err != nil {
				return err
			}
			if _, ok := t.(ixml.EndElement); !ok {
				return fmt.Errorf("unexpected token %T", t)
			}
			*pn = append(*pn, xml.Name(name))
		}
	}
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_propfind
type propfind struct {
	XMLName  ixml.Name     `xml:"DAV: propfind"`
	Allprop  *struct{}     `xml:"DAV: allprop"`
	Propname *struct{}     `xml:"DAV: propname"`
	Prop     propfindProps `xml:"DAV: prop"`
	Include  propfindProps `xml:"DAV: include"`
}

func readPropfind(r io.Reader) (pf propfind, status int, err error) {
	c := countingReader{r: r}
	if err = ixml.NewDecoder(&c).Decode(&pf); err != nil {
		if err == io.EOF {
			if c.n == 0 {
				// An empty body means to propfind allprop.
				// http://www.webdav.org/specs/rfc4918.html#METHOD_PROPFIND
				return propfind{Allprop: new(struct{})}, 0, nil
			}
			err = errInvalidPropfind
		}
		return propfind{}, http.StatusBadRequest, err
	}

	if pf.Allprop == nil && pf.Include != nil {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	if pf.Allprop != nil && (pf.Prop != nil || pf.Propname != nil) {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	if pf.Prop != nil && pf.Propname != nil {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	if pf.Propname == nil && pf.Allprop == nil && pf.Prop == nil {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	return pf, 0, nil
}

// ixmlProperty is the same as the Property type except it holds an ixml.Name
// instead of an xml.Name.
type ixmlProperty struct {
	XMLName  ixml.Name
	Lang     string `xml:"xml:lang,attr,omitempty"`
	InnerXML []byte `xml:",innerxml"`
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_error
// See multistatusWriter for the "D:" namespace prefix.
type xmlError struct {
	XMLName  ixml.Name `xml:"D:error"`
	InnerXML []byte    `xml:",innerxml"`
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_propstat
// See multistatusWriter for the "D:" namespace prefix.
type propstat struct {
	Prop                []webdav.Property `xml:"D:prop>_ignored_"`
	Status              string            `xml:"D:status"`
	Error               *xmlError         `xml:"D:error"`
	ResponseDescription string            `xml:"D:responsedescription,omitempty"`
}

// ixmlPropstat is the same as the propstat type except it holds an ixml.Name
// instead of an xml.Name.
type ixmlPropstat struct {
	Prop                []ixmlProperty `xml:"D:prop>_ignored_"`
	Status              string         `xml:"D:status"`
	Error               *xmlError      `xml:"D:error"`
	ResponseDescription string         `xml:"D:responsedescription,omitempty"`
}

// MarshalXML prepends the "D:" namespace prefix on properties in the DAV: namespace
// before encoding. See multistatusWriter.
func (ps propstat) MarshalXML(e *ixml.Encoder, start ixml.StartElement) error {
	// Convert from a propstat to an ixmlPropstat.
	ixmlPs := ixmlPropstat{
		Prop:                make([]ixmlProperty, len(ps.Prop)),
		Status:              ps.Status,
		Error:               ps.Error,
		ResponseDescription: ps.ResponseDescription,
	}
	for k, prop := range ps.Prop {
		ixmlPs.Prop[k] = ixmlProperty{
			XMLName:  ixml.Name(prop.XMLName),
			Lang:     prop.Lang,
			InnerXML: prop.InnerXML,
		}
	}

	for k, prop := range ixmlPs.Prop {
		if prop.XMLName.Space == "DAV:" {
			prop.XMLName = ixml.Name{Space: "", Local: "D:" + prop.XMLName.Local}
			ixmlPs.Prop[k] = prop
		}
	}
	// Distinct type to avoid infinite recursion of MarshalXML.
	type newpropstat ixmlPropstat
	return e.EncodeElement(newpropstat(ixmlPs), start)
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_response
// See multistatusWriter for the "D:" namespace prefix.
type response struct {
	XMLName             ixml.Name  `xml:"D:response"`
	Href                []string   `xml:"D:href"`
	Propstat            []propstat `xml:"D:propstat"`
	Status              string     `xml:"D:status,omitempty"`
	Error               *xmlError  `xml:"D:error"`
	ResponseDescription string     `xml:"D:responsedescription,omitempty"`
}

// MultistatusWriter marshals one or more Responses into a XML
// multistatus response.
// See http://www.webdav.org/specs/rfc4918.html#ELEMENT_multistatus
// TODO(rsto, mpl): As a workaround, the "D:" namespace prefix, defined as
// "DAV:" on this element, is prepended on the nested response, as well as on all
// its nested elements. All property names in the DAV: namespace are prefixed as
// well. This is because some versions of Mini-Redirector (on windows 7) ignore
// elements with a default namespace (no prefixed namespace). A less intrusive fix
// should be possible after golang.org/cl/11074. See https://golang.org/issue/11177
type multistatusWriter struct {
	// ResponseDescription contains the optional responsedescription
	// of the multistatus XML element. Only the latest content before
	// close will be emitted. Empty response descriptions are not
	// written.
	responseDescription string

	w   http.ResponseWriter
	enc *ixml.Encoder
}

// Write validates and emits a DAV response as part of a multistatus response
// element.
//
// It sets the HTTP status code of its underlying http.ResponseWriter to 207
// (Multi-Status) and populates the Content-Type header. If r is the
// first, valid response to be written, Write prepends the XML representation
// of r with a multistatus tag. Callers must call close after the last response
// has been written.
func (w *multistatusWriter) write(r *response) error {
	switch len(r.Href) {
	case 0:
		return errInvalidResponse
	case 1:
		if len(r.Propstat) > 0 != (r.Status == "") {
			return errInvalidResponse
		}
	default:
		if len(r.Propstat) > 0 || r.Status == "" {
			return errInvalidResponse
		}
	}
	err := w.writeHeader()
	if err != nil {
		return err
	}
	return w.enc.Encode(r)
}

// writeHeader writes a XML multistatus start element on w's underlying
// http.ResponseWriter and returns the result of the write operation.
// After the first write attempt, writeHeader becomes a no-op.
func (w *multistatusWriter) writeHeader() error {
	if w.enc != nil {
		return nil
	}
	w.w.Header().Add("Content-Type", "text/xml; charset=utf-8")
	w.w.WriteHeader(webdav.StatusMulti)
	_, err := fmt.Fprintf(w.w, `<?xml version="1.0" encoding="UTF-8"?>`)
	if err != nil {
		return err
	}
	w.enc = ixml.NewEncoder(w.w)
	return w.enc.EncodeToken(ixml.StartElement{
		Name: ixml.Name{
			Space: "DAV:",
			Local: "multistatus",
		},
		Attr: []ixml.Attr{{
			Name:  ixml.Name{Space: "xmlns", Local: "D"},
			Value: "DAV:",
		}},
	})
}

// Close completes the marshalling of the multistatus response. It returns
// an error if the multistatus response could not be completed. If both the
// return value and field enc of w are nil, then no multistatus response has
// been written.
func (w *multistatusWriter) close() error {
	if w.enc == nil {
		return nil
	}
	var end []ixml.Token
	if w.responseDescription != "" {
		name := ixml.Name{Space: "DAV:", Local: "responsedescription"}
		end = append(end,
			ixml.StartElement{Name: name},
			ixml.CharData(w.responseDescription),
			ixml.EndElement{Name: name},
		)
	}
	end = append(end, ixml.EndElement{
		Name: ixml.Name{Space: "DAV:", Local: "multistatus"},
	})
	for _, t := range end {
		err := w.enc.EncodeToken(t)
		if err != nil {
			return err
		}
	}
	return w.enc.Flush()
}
