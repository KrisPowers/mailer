package message

import (
	"slices"
	"testing"
)

func TestRiskyFiles(t *testing.T) {
	raw := []byte("Content-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nhi\r\n" +
		"--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"report.pdf\"\r\n\r\nx\r\n" +
		"--b\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"invoice.pdf.exe\"\r\n\r\nx\r\n" +
		"--b\r\nContent-Type: application/vnd.ms-excel.sheet.macroEnabled.12; name=\"Q3.XLSM\"\r\n\r\nx\r\n" +
		"--b--\r\n")
	if got := RiskyFiles(raw); !slices.Equal(got, []string{"invoice.pdf.exe", "Q3.XLSM"}) {
		t.Errorf("RiskyFiles = %q", got)
	}
	if got := RiskyFiles([]byte("Subject: hi\r\n\r\nbody\r\n")); got != nil {
		t.Errorf("plain message: %q", got)
	}
}

func TestBulk(t *testing.T) {
	for raw, want := range map[string]bool{
		"List-Unsubscribe: <mailto:u@example.com>\r\n\r\n": true,
		"Precedence: bulk\r\n\r\n":                         true,
		"Subject: hello\r\n\r\n":                           false,
	} {
		hdr, _ := SplitHeader([]byte(raw))
		if got := Bulk(ParseHeader(hdr)); got != want {
			t.Errorf("%q: Bulk = %v", raw, got)
		}
	}
}
