package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// covers: MA-285, R17, S35
// sensitivityFromExtProps decodes the PidTagSensitivity extended-property value
// (MAPI integer) to the model sensitivity, regardless of how the service spells the
// returned id; 0/absent/garbage → "" (Normal).
func TestSensitivityFromExtProps(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{"1", "personal"}, {"2", "private"}, {"3", "confidential"},
		{"0", ""}, {"", ""}, {"9", ""}, {"garbage", ""},
	}
	for _, c := range cases {
		got := sensitivityFromExtProps([]singleValueExtProp{{ID: "Integer 0x36", Value: c.value}})
		if got != c.want {
			t.Errorf("value %q → %q, want %q", c.value, got, c.want)
		}
	}
	if got := sensitivityFromExtProps(nil); got != "" {
		t.Errorf("absent property → %q, want \"\" (Normal)", got)
	}
}

// covers: MA-285, R17, S35
// When the tenant supports it, the listing $expand carries PidTagSensitivity and
// Messages decodes it onto MessageRef.Sensitivity — and the request actually asked
// for the extended property.
func TestMessagesCapturesSensitivityViaExpand(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			w.Write([]byte(`{"access_token":"t","token_type":"Bearer","expires_in":3600}`))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			gotQuery = r.URL.RawQuery
			// Return the extended property with a DIFFERENTLY-spelled id ("Integer
			// 0x36" vs our requested "Integer 0x0036") to prove we match by value.
			w.Write([]byte(`{"value":[{"id":"M1","internetMessageId":"<m1@x>","subject":"s","receivedDateTime":"2025-03-01T09:00:00Z","singleValueExtendedProperties":[{"id":"Integer 0x36","value":"2"}]}]}`))
		default:
			w.Write([]byte(`{"value":[]}`))
		}
	}))
	defer srv.Close()

	c := New(context.Background(), Config{Tenant: "t", ClientID: "c", ClientSecret: "s", BaseURL: srv.URL, TokenURL: srv.URL + "/token"})
	var refs []MessageRef
	if err := c.Messages(context.Background(), "u1", "F_IN", func(r MessageRef) error { refs = append(refs, r); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Sensitivity != "private" {
		t.Fatalf("Sensitivity = %q (refs=%d), want \"private\"", refOrEmpty(refs), len(refs))
	}
	if !strings.Contains(gotQuery, "singleValueExtendedProperties") || !strings.Contains(gotQuery, "0x0036") {
		t.Errorf("listing request did not $expand PidTagSensitivity: %q", gotQuery)
	}
}

// covers: MA-285, R17, S35
// Best-effort: a tenant/endpoint that REJECTS the sensitivity $expand must not fail
// the capture. Messages retries the first page WITHOUT the $expand and completes —
// every message is still returned, just with no sensitivity. This is the guarantee
// for an Exchange that does not support (or use) the extended property.
func TestMessagesSensitivityExpandDegradesGracefully(t *testing.T) {
	var mu sync.Mutex
	sawExpand, sawPlain := false, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"access_token":"t","token_type":"Bearer","expires_in":3600}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages") {
			if strings.Contains(r.URL.RawQuery, "expand=") {
				mu.Lock()
				sawExpand = true
				mu.Unlock()
				// Simulate a tenant that does not support the extended-property expand.
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":{"code":"ErrorInvalidProperty","message":"The expand expression is not supported."}}`))
				return
			}
			mu.Lock()
			sawPlain = true
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"value":[{"id":"M1","internetMessageId":"<m1@x>","subject":"s","receivedDateTime":"2025-03-01T09:00:00Z"},{"id":"M2","internetMessageId":"<m2@x>","subject":"s2","receivedDateTime":"2025-03-02T09:00:00Z"}]}`))
			return
		}
		w.Write([]byte(`{"value":[]}`))
	}))
	defer srv.Close()

	c := New(context.Background(), Config{Tenant: "t", ClientID: "c", ClientSecret: "s", BaseURL: srv.URL, TokenURL: srv.URL + "/token"})
	var refs []MessageRef
	err := c.Messages(context.Background(), "u1", "F_IN", func(r MessageRef) error { refs = append(refs, r); return nil })
	if err != nil {
		t.Fatalf("capture must succeed when the $expand is unsupported, got: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("all messages must still be captured; got %d, want 2", len(refs))
	}
	for _, r := range refs {
		if r.Sensitivity != "" {
			t.Errorf("message %s: Sensitivity = %q, want \"\" when the $expand was unsupported", r.ID, r.Sensitivity)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !sawExpand || !sawPlain {
		t.Errorf("expected a first $expand attempt (rejected) then a no-$expand retry; sawExpand=%v sawPlain=%v", sawExpand, sawPlain)
	}
}

func refOrEmpty(refs []MessageRef) string {
	if len(refs) > 0 {
		return refs[0].Sensitivity
	}
	return "<none>"
}
