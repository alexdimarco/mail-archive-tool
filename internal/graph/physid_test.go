package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// physIDServer serves one Inbox message; when honorPrefer is true it honors the
// immutable-id preference (emits Preference-Applied and returns an immutable id),
// otherwise it ignores the header and returns a mutable id.
func physIDServer(honorPrefer bool, msgID string) *httptest.Server {
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(s))
	}
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		if honorPrefer && strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "immutableid") {
			w.Header().Set("Preference-Applied", `IdType="ImmutableId"`)
		}
		j(w, `{"value":[{"id":"`+msgID+`","internetMessageId":"<m@x>","subject":"s","receivedDateTime":"2025-03-01T09:00:00Z"}]}`)
	})
	return httptest.NewServer(mux)
}

// covers: MA-241, R17, S38, S39
// The Graph client requests immutable ids (Prefer: IdType="ImmutableId") and sets
// MessageRef.PhysID to the message id ONLY when the tenant HONORS it (the
// Preference-Applied header is present) — the namespace is decided once from the
// listing. A tenant that ignores the preference leaves PhysID empty, so the live
// path degrades to Message-ID membership (the floor), never mistaking a mutable id
// for an immutable one.
func TestMessagesCapturesPhysIDWhenHonored(t *testing.T) {
	collect := func(srv *httptest.Server) []MessageRef {
		defer srv.Close()
		c := New(context.Background(), Config{Tenant: "t", ClientID: "c", ClientSecret: "s", BaseURL: srv.URL, TokenURL: srv.URL + "/token"})
		var refs []MessageRef
		if err := c.Messages(context.Background(), "u1", "F_IN", func(r MessageRef) error { refs = append(refs, r); return nil }); err != nil {
			t.Fatal(err)
		}
		return refs
	}
	honored := collect(physIDServer(true, "IMMUTABLE-1"))
	if len(honored) != 1 || honored[0].PhysID != "IMMUTABLE-1" {
		t.Errorf("honored tenant: PhysID = %q, want the immutable id IMMUTABLE-1", func() string {
			if len(honored) > 0 {
				return honored[0].PhysID
			}
			return "<none>"
		}())
	}
	withheld := collect(physIDServer(false, "MUTABLE-1"))
	if len(withheld) != 1 || withheld[0].PhysID != "" {
		t.Errorf("tenant that ignored the preference: PhysID = %q, want empty (degrade to floor)", func() string {
			if len(withheld) > 0 {
				return withheld[0].PhysID
			}
			return "<none>"
		}())
	}
}
