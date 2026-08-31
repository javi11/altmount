package api

import (
	"strings"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/nzb"
)

// buildStoreNZB renders a minimal store-backed NZB (no <head>), mirroring what
// the export path receives from StoreService.RegenerateNZB.
func buildStoreNZB() []byte {
	return nzb.BuildNZB(&metapb.NzbStore{Files: []*metapb.NzbFileEntry{
		{
			Subject:  "Release.part01.rar",
			Poster:   "poster@example.com",
			Date:     1700000000,
			Groups:   []string{"alt.binaries.test"},
			Segments: []*metapb.NzbSeg{{Id: "abc@host", Number: 1, Bytes: 1000}},
		},
	}})
}

func TestInjectEncryptionMeta_Password(t *testing.T) {
	s := &Server{}
	out := string(s.injectEncryptionMeta(buildStoreNZB(), &metapb.FileMetadata{
		Password: "s3cret",
	}))

	if !strings.Contains(out, `<meta type="password">s3cret</meta>`) {
		t.Errorf("password meta not embedded; got:\n%s", out)
	}
	// The <head> must come before the first <file> to be valid.
	if strings.Index(out, "<head>") > strings.Index(out, "<file") {
		t.Errorf("<head> must precede <file>; got:\n%s", out)
	}
}

func TestInjectEncryptionMeta_PasswordWithDollar(t *testing.T) {
	// A "$" in the password must survive verbatim (ReplaceAllStringFunc, not
	// ReplaceAllString which would interpret "$1" etc.).
	s := &Server{}
	pw := `a$1b$name`
	out := string(s.injectEncryptionMeta(buildStoreNZB(), &metapb.FileMetadata{Password: pw}))

	if !strings.Contains(out, "<meta type=\"password\">"+pw+"</meta>") {
		t.Errorf("password with $ not preserved verbatim; got:\n%s", out)
	}
}

func TestInjectEncryptionMeta_AESCipherAndSalt(t *testing.T) {
	s := &Server{}
	out := string(s.injectEncryptionMeta(buildStoreNZB(), &metapb.FileMetadata{
		Encryption: metapb.Encryption_AES,
		Password:   "pw",
		Salt:       "deadbeef",
	}))

	for _, want := range []string{
		`<meta type="cipher">`,
		`<meta type="password">pw</meta>`,
		`<meta type="salt">deadbeef</meta>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestInjectEncryptionMeta_NoopWhenUnprotected(t *testing.T) {
	s := &Server{}
	in := buildStoreNZB()
	out := s.injectEncryptionMeta(in, &metapb.FileMetadata{Encryption: metapb.Encryption_NONE})
	if string(out) != string(in) {
		t.Errorf("expected no change for unencrypted, unprotected file")
	}
	if strings.Contains(string(out), "<head>") {
		t.Errorf("no <head> should be added when there is nothing to embed")
	}
}
