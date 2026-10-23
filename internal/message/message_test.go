package message

import (
	"bytes"
	"testing"
)

const multipart = "From: a@b.c\r\nContent-Type: multipart/mixed; boundary=\"XX\"\r\n\r\n" +
	"preamble\r\n--XX\r\nContent-Type: text/plain\r\n\r\nhello\r\nworld\r\n--XX\r\n" +
	"Content-Type: message/rfc822\r\n\r\nSubject: inner\r\n\r\ninner body\r\n--XX--\r\nepilogue\r\n"

func TestParseMultipart(t *testing.T) {
	p := Parse([]byte(multipart))
	if !p.IsMultipart() || len(p.Children) != 2 {
		t.Fatalf("got %d children", len(p.Children))
	}
	if got := string(p.Children[0].Body); got != "hello\r\nworld" {
		t.Errorf("part 1 body = %q", got)
	}
	inner := p.Children[1].Embedded
	if inner == nil || inner.Header.Get("Subject") != "inner" || string(inner.Body) != "inner body" {
		t.Errorf("embedded message parsed wrong: %+v", inner)
	}
}

func TestHeaderFolding(t *testing.T) {
	h := ParseHeader([]byte("Subject: one\r\n two\r\nX: y\r\n\r\n"))
	if len(h) != 2 || h.Get("subject") != "one two" {
		t.Fatalf("got %+v", h)
	}
	if !bytes.Equal(h[0].Raw, []byte("Subject: one\r\n two\r\n")) {
		t.Errorf("raw = %q", h[0].Raw)
	}
}

func TestNormalizeCRLF(t *testing.T) {
	if got := string(NormalizeCRLF([]byte("a\nb\r\nc\rd"))); got != "a\r\nb\r\nc\r\nd" {
		t.Errorf("got %q", got)
	}
}

func TestDecodedBody(t *testing.T) {
	p := Parse([]byte("Content-Transfer-Encoding: base64\r\n\r\naGVsbG8g\r\nd29ybGQ=\r\n"))
	if got := string(p.DecodedBody()); got != "hello world" {
		t.Errorf("got %q", got)
	}
}
