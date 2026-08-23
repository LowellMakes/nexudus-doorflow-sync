package nexudusapi

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// Compile-time assertion that Client satisfies the domain interface.
var _ domain.Nexudus = (*Client)(nil)

// coworkerDTO is the subset of the Nexudus coworker we consume for sync.
// Field names match the API's wire format (PascalCase) as documented.
type coworkerDTO struct {
	ID                  int    `json:"Id"`
	UserID              int    `json:"UserId"`
	GuessedFirstName    string `json:"GuessedFirstName"`
	GuessedLastName     string `json:"GuessedLastName"`
	Email               string `json:"Email"`
	TeamIds             string `json:"TeamIds"`
	CoworkerContractIds string `json:"CoworkerContractIds"`
	AccessPincode       string `json:"AccessPincode"`
	KeyFobNumber        string `json:"KeyFobNumber"`
	AccessCardId        string `json:"AccessCardId"`
}

type coworkerPage struct {
	Content []coworkerDTO `json:"Content"`
	HasMore bool          `json:"HasMore"`
}

type invoiceDTO struct {
	CoworkerId int `json:"CoworkerId"`
}

type invoicePage struct {
	Content []invoiceDTO `json:"Content"`
	HasMore bool         `json:"HasMore"`
}

func (c *Client) GetCoworker(ctx context.Context, id int) (*domain.Coworker, error) {
	var dto coworkerDTO
	path := "/api/spaces/coworkers/" + strconv.Itoa(id)
	if err := c.do(ctx, "GET", path, nil, &dto); err != nil {
		return nil, err
	}
	return toDomain(dto), nil
}

func (c *Client) ContractsAndInvoices(ctx context.Context, coworkerID int) (int, bool, error) {
	cw, err := c.GetCoworker(ctx, coworkerID)
	if err != nil {
		return 0, false, err
	}
	q := url.Values{}
	q.Set("coworkerId", strconv.Itoa(coworkerID))
	q.Set("status", "overdue")
	q.Set("size", "1")
	var page invoicePage
	if err := c.do(ctx, "GET", "/api/billing/coworkerinvoices?"+q.Encode(), nil, &page); err != nil {
		return cw.ContractCount, false, err
	}
	return cw.ContractCount, len(page.Content) > 0, nil
}

func (c *Client) UpdatedCoworkersSince(ctx context.Context, since time.Time, page, size int) ([]*domain.Coworker, bool, error) {
	q := url.Values{}
	q.Set("from_Coworker_UpdatedOn", since.Format(time.RFC3339))
	q.Set("page", strconv.Itoa(page))
	q.Set("size", strconv.Itoa(size))
	var wp coworkerPage
	if err := c.do(ctx, "GET", "/api/spaces/coworkers?"+q.Encode(), nil, &wp); err != nil {
		return nil, false, err
	}
	out := make([]*domain.Coworker, 0, len(wp.Content))
	for _, cw := range wp.Content {
		out = append(out, toDomain(cw))
	}
	return out, wp.HasMore || (size > 0 && len(wp.Content) >= size), nil
}

func (c *Client) AllCoworkers(ctx context.Context, page, size int) ([]*domain.Coworker, bool, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("size", strconv.Itoa(size))
	var wp coworkerPage
	if err := c.do(ctx, "GET", "/api/spaces/coworkers?"+q.Encode(), nil, &wp); err != nil {
		return nil, false, err
	}
	out := make([]*domain.Coworker, 0, len(wp.Content))
	for _, cw := range wp.Content {
		out = append(out, toDomain(cw))
	}
	return out, wp.HasMore || (size > 0 && len(wp.Content) >= size), nil
}

func (c *Client) PaidMembersSince(ctx context.Context, since time.Time, page, size int) ([]int, bool, error) {
	q := url.Values{}
	q.Set("from_Coworker_Invoice_PaidOn", since.Format(time.RFC3339))
	q.Set("status", "paid")
	q.Set("page", strconv.Itoa(page))
	q.Set("size", strconv.Itoa(size))
	var wp invoicePage
	if err := c.do(ctx, "GET", "/api/billing/coworkerinvoices?"+q.Encode(), nil, &wp); err != nil {
		return nil, false, err
	}
	ids := make([]int, 0, len(wp.Content))
	for _, inv := range wp.Content {
		ids = append(ids, inv.CoworkerId)
	}
	return ids, wp.HasMore || (size > 0 && len(wp.Content) >= size), nil
}

// UpdateCoworkerPin performs a read-modify-write of the full coworker object,
// changing only the AccessPincode field so unrelated fields are never clobbered.
func (c *Client) UpdateCoworkerPin(ctx context.Context, coworkerID int, pin string) error {
	path := "/api/spaces/coworkers/" + strconv.Itoa(coworkerID)
	var raw map[string]any
	if err := c.do(ctx, "GET", path, nil, &raw); err != nil {
		return err
	}
	raw["AccessPincode"] = pin
	if err := c.do(ctx, "PUT", "/api/spaces/coworkers", raw, nil); err != nil {
		return err
	}
	return nil
}

func (c *Client) SendPinReminder(ctx context.Context, coworkerID int) error {
	return c.do(ctx, "POST", "/api/spaces/coworkers/"+strconv.Itoa(coworkerID)+"/sendpinreminder", nil, nil)
}

func toDomain(c coworkerDTO) *domain.Coworker {
	return &domain.Coworker{
		ID:            c.ID,
		UserID:        c.UserID,
		FirstName:     c.GuessedFirstName,
		LastName:      c.GuessedLastName,
		Email:         c.Email,
		TeamIDs:       parseInts(c.TeamIds),
		ContractCount: countItems(c.CoworkerContractIds),
		AccessPin:     c.AccessPincode,
		KeyFob:        c.KeyFobNumber,
		AccessCard:    c.AccessCardId,
	}
}

func parseInts(s string) []int32 {
	parts := strings.Split(s, ",")
	out := make([]int32, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err == nil {
			out = append(out, int32(n))
		}
	}
	return out
}

func countItems(s string) int {
	parts := strings.Split(s, ",")
	n := 0
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	return n
}
