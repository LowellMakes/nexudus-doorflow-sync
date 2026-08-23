package nexudusapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(srv *httptest.Server) *Client {
	return New(srv.URL, "user", "pass", slog.Default(), 5*time.Second)
}

func TestGetCoworkerMapsExpectedFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/spaces/coworkers/42" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		// Basic-auth must be sent.
		u, p, _ := r.BasicAuth()
		if u != "user" || p != "pass" {
			t.Errorf("basic auth not sent: %q %q", u, p)
		}
		io.WriteString(w, `{
			"Id": 42,
			"UserId": 7,
			"Email": "jo@example.com",
			"GuessedFirstName": "Jo",
			"GuessedLastName": "Doe",
			"TeamIds": "111,222",
			"CoworkerContractIds": "c1,c2",
			"AccessPincode": "1234",
			"KeyFobNumber": "FOB-9",
			"AccessCardId": "CARD-5"
		}`)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	cw, err := c.GetCoworker(t.Context(), 42)
	if err != nil {
		t.Fatalf("GetCoworker: %v", err)
	}
	if cw.ID != 42 || cw.UserID != 7 || cw.Email != "jo@example.com" {
		t.Errorf("identity not mapped: %+v", cw)
	}
	if cw.AccessPin != "1234" || cw.KeyFob != "FOB-9" || cw.AccessCard != "CARD-5" {
		t.Errorf("credentials not mapped: %+v", cw)
	}
	if len(cw.TeamIDs) != 2 || cw.TeamIDs[0] != 111 || cw.TeamIDs[1] != 222 {
		t.Errorf("teams not mapped: %v", cw.TeamIDs)
	}
	if cw.ContractCount != 2 {
		t.Errorf("contract count = %d, want 2", cw.ContractCount)
	}
	if cw.Contact() {
		t.Errorf("cw should not be a contact (UserId=7)")
	}
}

func TestUpdateCoworkerPinPreservesOtherFields(t *testing.T) {
	original := map[string]any{
		"Id": 42, "UserId": 7, "Email": "jo@example.com",
		"GuessedFirstName": "Jo", "GuessedLastName": "Doe",
		"Note":          "preserve me",
		"BilledMonthly": true,
		"AccessPincode": "oldpin",
	}
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/spaces/coworkers/42":
			json.NewEncoder(w).Encode(original)
		case r.Method == http.MethodPut && r.URL.Path == "/api/spaces/coworkers":
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &putBody)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/spaces/coworkers/42/sendpinreminder":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 400)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	ctx := t.Context()
	if err := c.UpdateCoworkerPin(ctx, 42, "NEWPIN"); err != nil {
		t.Fatalf("UpdateCoworkerPin: %v", err)
	}
	if putBody == nil {
		t.Fatal("PUT /api/spaces/coworkers was not issued")
	}
	if got, _ := putBody["AccessPincode"].(string); got != "NEWPIN" {
		t.Errorf("AccessPincode = %v, want NEWPIN; body=%v", putBody["AccessPincode"], putBody)
	}
	if got, _ := putBody["Note"].(string); got != "preserve me" {
		t.Errorf("unrelated field 'Note' was clobbered: %v", putBody)
	}
	if got, _ := putBody["BilledMonthly"].(bool); !got {
		t.Errorf("unrelated field 'BilledMonthly' was clobbered: %v", putBody)
	}
	if got, _ := putBody["UserId"].(float64); got != 7 {
		t.Errorf("unrelated field 'UserId' was clobbered: %v", putBody)
	}

	// sendpinreminder must hit the documented endpoint.
	if err := c.SendPinReminder(ctx, 42); err != nil {
		t.Fatalf("SendPinReminder: %v", err)
	}
}
