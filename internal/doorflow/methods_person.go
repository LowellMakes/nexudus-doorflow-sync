package doorflow

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

func (c *Client) FindPersonByEmail(ctx context.Context, email string) (*domain.Person, error) {
	q := url.Values{"email": {email}}
	var p person
	if err := c.get(ctx, "/api/2/people", &p, q); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.status == http.StatusNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if p.ID == 0 {
		return nil, domain.ErrNotFound
	}
	return toDomainPerson(&p), nil
}

func (c *Client) FindPersonBySystemID(ctx context.Context, systemID string) (*domain.Person, error) {
	q := url.Values{"system_id": {systemID}}
	var p person
	if err := c.get(ctx, "/api/2/people", &p, q); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.status == http.StatusNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if p.ID == 0 {
		return nil, domain.ErrNotFound
	}
	return toDomainPerson(&p), nil
}

func (c *Client) CreatePerson(ctx context.Context, p *domain.Person) (*domain.Person, error) {
	var out person
	if err := c.do(ctx, "POST", "/api/2/people", fromDomainPerson(p), &out); err != nil {
		return nil, err
	}
	return toDomainPerson(&out), nil
}

func (c *Client) UpdatePerson(ctx context.Context, id int, p *domain.Person) (*domain.Person, error) {
	path := "/api/2/person/" + strconv.Itoa(id)
	var out person
	if err := c.do(ctx, "PUT", path, fromDomainPerson(p), &out); err != nil {
		return nil, err
	}
	return toDomainPerson(&out), nil
}
