package graph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// covers: MA-402, R17, R29
// MIMETo is the one bounded $value download both consumers share: it streams the
// body byte-exact into a writer, stops on idle, on the size cap and on a cancel
// with no partial output, and runs its own header timer of at least the idle
// window; StatusError is {Code, GraphCode, Text} with Error() unchanged.
func TestMIMEToContract(t *testing.T) {
	t.Skip("MA-402: MIMETo, its typed stops and the typed StatusError do not exist yet; lands in FI-SG")
}

// covers: MA-402, R17, R29
// A non-success Graph response is a typed *StatusError{Code, GraphCode, Text}:
// callers tell 404/401/403/500 apart with errors.As, GraphCode is the body's
// error.code, Text is bounded, and Error() is byte-for-byte today's text ("graph
// " + the status line + ": " + the trimmed body, at most 2048 bytes), which the
// last-run record, status's remedy needles and the CLI read. The listing path
// and the $value path return the same type.
func TestStatusErrorIsTypedAndKeepsItsText(t *testing.T) {
	long := `{"error":{"code":"ErrorInternalServerError","message":"` + strings.Repeat("x", 5000) + `"}}`
	bodies := map[string]struct {
		code int
		body string
	}{
		"M404":  {http.StatusNotFound, `{"error":{"code":"ErrorItemNotFound","message":"The specified object was not found in the store."}}`},
		"M401":  {http.StatusUnauthorized, `{"error":{"code":"InvalidAuthenticationToken","message":"AADSTS700082: the credential has expired"}}`},
		"M403":  {http.StatusForbidden, `{"error":{"code":"ErrorAccessDenied","message":"Access is denied. Check credentials and try again."}}`},
		"M500":  {http.StatusInternalServerError, `{"error":{"code":"ErrorMimeContentConversionFailed","message":"The MIME content conversion failed."}}`},
		"M502":  {http.StatusBadGateway, "  <html><body>Bad Gateway</body></html>\r\n"},
		"MLONG": {http.StatusInternalServerError, long},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u/messages/"), "/$value")
		b, ok := bodies[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(b.code)
		io.WriteString(w, b.body)
	})
	mux.HandleFunc("/users/u/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"code":"ErrorAccessDenied","message":"Access is denied."}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)
	ctx := context.Background()

	// todaysText is the client's text before StatusError: "graph " + the
	// response's status line + ": " + the first 2048 body bytes, trimmed.
	todaysText := func(path string) string {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "graph " + resp.Status + ": " + strings.TrimSpace(string(b))
	}

	wantGraphCode := map[string]string{
		"M404": "ErrorItemNotFound", "M401": "InvalidAuthenticationToken",
		"M403": "ErrorAccessDenied", "M500": "ErrorMimeContentConversionFailed", "M502": "",
	}
	for id, b := range bodies {
		_, err := c.MIME(ctx, "u", id)
		var se *StatusError
		if !errors.As(err, &se) {
			t.Fatalf("%s: a %d $value response is not a *StatusError: %v", id, b.code, err)
		}
		if se.Code != b.code {
			t.Errorf("%s: Code = %d, want %d", id, se.Code, b.code)
		}
		if want, ok := wantGraphCode[id]; ok && se.GraphCode != want {
			t.Errorf("%s: GraphCode = %q, want %q", id, se.GraphCode, want)
		}
		if len(se.Text) > statusTextMax {
			t.Errorf("%s: Text is %d bytes, want at most %d (bounded)", id, len(se.Text), statusTextMax)
		}
		if want := todaysText("/users/u/messages/" + id + "/$value"); err.Error() != want {
			t.Errorf("%s: Error() changed:\n got %q\nwant %q", id, clip(err.Error()), clip(want))
		}
	}
	if b := bodies["MLONG"]; len(b.body) <= statusTextMax {
		t.Fatal("the long body must exceed the bound for the bounded-Text case to mean anything")
	}

	// The listing path returns the same type, and errors.As sees through a
	// caller's wrapping (capture wraps it as "list folders: %w").
	_, err := c.Folders(ctx, "u", FolderFilter{IncludeDeleted: true, IncludeJunk: true})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusForbidden || se.GraphCode != "ErrorAccessDenied" {
		t.Fatalf("a 403 listing is not a typed *StatusError{403, ErrorAccessDenied}: %v (%+v)", err, se)
	}
	if want := todaysText("/users/u/mailFolders"); err.Error() != want {
		t.Errorf("listing Error() changed:\n got %q\nwant %q", clip(err.Error()), clip(want))
	}
}

// clip shortens a long string for a failure message.
func clip(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// covers: MA-314, R10, R17
// The primitive download bounds, scaled: a trickle inside the idle window
// completes, a mid-body stall and the overall cap stop with typed errors while
// the caller's context stays live, and the size cap holds at its boundary.
func TestMIMEToBounds(t *testing.T) {
	t.Skip("MA-314: MIMETo's idle watchdog, overall cap and size cap do not exist yet; lands in FI-SG")
}
