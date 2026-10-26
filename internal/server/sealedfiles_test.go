package server

import (
	"bufio"
	"bytes"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-796, R19
// An encrypted archive's original is served as message/rfc822 whatever its bytes
// begin with — never sniffed into a page (AO3) — and an attachment zip as
// application/zip.
func TestSealedFilesPinTheirTypes(t *testing.T) {
	// A host whose type table calls .eml a page (the pin's reason): set here, so the
	// test does not depend on this host's own table.
	if err := mime.AddExtensionType(".eml", "text/html"); err != nil {
		t.Fatal(err)
	}
	defer mime.AddExtensionType(".eml", "message/rfc822")
	open := func(r *http.Request, rel string) ([]byte, func(), error) {
		return []byte("<html><script>alert(1)</script>"), func() {}, nil
	}
	ts := httptest.NewServer(http.StripPrefix("/files/", SealedFiles(open, func() string { return "" }, time.Minute)))
	defer ts.Close()
	for path, want := range map[string]string{"/files/a/m.eml": "message/rfc822", "/files/a/m-attachments.zip": "application/zip"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Content-Type"); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	assure.Reached(t, true, "both types judged")
}

// covers: MA-796, R37
// A client that stops reading cannot hold a file's memory past the write deadline:
// the response ends and the bytes' release runs (§3.2).
func TestASealedFileClientThatStopsReadingIsLetGo(t *testing.T) {
	var released atomic.Bool
	big := bytes.Repeat([]byte("x"), 64<<20)
	open := func(r *http.Request, rel string) ([]byte, func(), error) {
		return big, func() { released.Store(true) }, nil
	}
	ts := httptest.NewServer(http.StripPrefix("/files/", SealedFiles(open, func() string { return "" }, 200*time.Millisecond)))
	defer ts.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("GET /files/a/m.eml HTTP/1.1\r\nHost: x\r\n\r\n"))
	bufio.NewReader(conn).Peek(1) // the response started; then nothing more is read
	deadline := time.Now().Add(20 * time.Second)
	for !released.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	assure.Reached(t, released.Load(), "the memory given back after the write deadline")
}
