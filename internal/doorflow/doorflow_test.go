package doorflow

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

func newTestClient(srv *httptest.Server, authType, key string) *Client {
	return New(srv.URL, authType, key, "", slog.Default(), 5*time.Second)
}

func TestPersonCreateAndUpdateUsePOSTandPUT(t *testing.T) {
	var calls []struct {
		method, path string
		body         string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, struct {
			method, path string
			body         string
		}{r.Method, r.URL.Path, string(b)})
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/2/people":
			io.WriteString(w, `{"id":12,"email":"a@b.c","first_name":"Jo","last_name":"Doe","enabled":true,"group_ids":[4482],"system_id":"NX42"}`)
		case r.Method == http.MethodPut && r.URL.Path == "/api/2/person/12":
			io.WriteString(w, `{"id":12,"email":"a@b.c","enabled":false,"group_ids":[4482],"system_id":"NX42"}`)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 400)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv, "basic", "k")
	ctx := t.Context()

	// new person -> POST
	p, err := c.CreatePerson(ctx, &domain.Person{Email: "a@b.c", FirstName: "Jo", LastName: "Doe",
		Enabled: true, GroupIDs: []int32{4482}, SystemID: "NX42"})
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	if p.ID != 12 || p.SystemID != "NX42" {
		t.Errorf("CreatePerson returned %v", p)
	}
	if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != "/api/2/people" {
		t.Fatalf("expected a POST /api/2/people call, got %v", calls)
	}
	for _, want := range []string{`"email":"a@b.c"`, `"enabled":true`, `"group_ids":[4482]`, `"system_id":"NX42"`} {
		if !strings.Contains(calls[0].body, want) {
			t.Errorf("POST body missing %q (got %q)", want, calls[0].body)
		}
	}

	// existing person -> PUT
	if _, err = c.UpdatePerson(ctx, 12, &domain.Person{Email: "a@b.c", Enabled: false, GroupIDs: []int32{4482}}); err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}
	if len(calls) != 2 || calls[1].method != "PUT" || calls[1].path != "/api/2/person/12" {
		t.Fatalf("expected a PUT /api/2/person/12 call, got %v", calls)
	}
}

func TestCredentialCreateUsesDiscoveredTypeIDAndInUseConflict(t *testing.T) {
	var createBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/3/credential_types":
			io.WriteString(w, `{"result":[{"id":101,"slug":"pin"},{"id":102,"slug":"keyfob"},{"id":103,"slug":"card"}]}`)
		case "/api/3/people/55/credentials":
			if strings.Contains(string(b), `"value":"TAKEN"`) {
				w.WriteHeader(409)
				io.WriteString(w, `{"error":"value already in use"}`)
				return
			}
			createBody = string(b)
			io.WriteString(w, `{"id":5,"credential_type_id":101,"value":"1234"}`)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 400)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv, "basic", "k")
	ctx := t.Context()
	if err := c.ResolveTypes(ctx); err != nil {
		t.Fatalf("ResolveTypes: %v", err)
	}
	// resolved type id for pin must be 101 (non-zero)
	id, err := c.typeID(domain.KindPin)
	if err != nil || id != 101 {
		t.Fatalf("pin type id = %d err=%v, want 101", id, err)
	}

	cred, err := c.CreateCredential(ctx, 55, domain.KindPin, "1234")
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	if cred.Kind != domain.KindPin || cred.Value != "1234" {
		t.Errorf("CreateCredential returned %v", cred)
	}
	if !strings.Contains(createBody, `"credential_type_id":101`) {
		t.Fatalf("create body did not use the discovered type id: %q", createBody)
	}

	// in-use conflict must surface as a distinct InUse error, not a generic 4xx
	_, err = c.CreateCredential(ctx, 55, domain.KindPin, "TAKEN")
	if !errorIsInUse(err) {
		t.Fatalf("expected an in-use error for a taken pin, got %v", err)
	}
}

func TestCreateCredentialRequiresResolvedTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", 500)
	}))
	defer srv.Close()
	c := newTestClient(srv, "basic", "k")
	if _, err := c.CreateCredential(t.Context(), 1, domain.KindPin, "1234"); err == nil {
		t.Fatal("expected an error when credential types are not resolved")
	}
}

func TestSyncIssuesCommitEndpoint(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := newTestClient(srv, "basic", "k")
	if err := c.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(got) != 1 || got[0] != "POST /api/2/sync" {
		t.Fatalf("expected exactly POST /api/2/sync, got %v", got)
	}
}

func errorIsInUse(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, domain.ErrInUse)
}
