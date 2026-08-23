package doorflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

func (c *Client) ListCredentials(ctx context.Context, personID int) ([]*domain.Credential, error) {
	var list credentialList
	path := fmt.Sprintf("/api/3/people/%d/credentials", personID)
	if err := c.get(ctx, path, &list, nil); err != nil {
		return nil, err
	}
	out := make([]*domain.Credential, 0, len(list.Result))
	c.mu.RLock()
	for _, cr := range list.Result {
		if k, ok := c.idToKind[cr.CredentialTypeID]; ok {
			out = append(out, &domain.Credential{ID: cr.ID, Kind: k, Value: cr.Value})
		}
	}
	c.mu.RUnlock()
	return out, nil
}

func (c *Client) CreateCredential(ctx context.Context, personID int, kind domain.CredentialKind, value string) (*domain.Credential, error) {
	if err := c.requireTypes(); err != nil {
		return nil, err
	}
	typeID, err := c.typeID(kind)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/3/people/%d/credentials", personID)
	body := &credentialReq{CredentialTypeID: typeID, Value: value}
	var out credential
	if err := c.do(ctx, "POST", path, body, &out); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.status == http.StatusConflict {
			if ae.inUse {
				return nil, &domain.InUseError{Kind: kind, Detail: ae.body}
			}
			// 409 "type already exists" is benign under idempotency: return the
			// existing credential of this kind.
			if creds, lerr := c.ListCredentials(ctx, personID); lerr == nil {
				for _, cr := range creds {
					if cr.Kind == kind {
						return cr, nil
					}
				}
			}
			return nil, domain.ErrAlreadyExists
		}
		return nil, err
	}
	return &domain.Credential{ID: out.ID, Kind: kind, Value: out.Value}, nil
}

// valueUpdate is the credential value-setting payload (PUT).
type valueUpdate struct {
	Value string `json:"value"`
}

func (c *Client) UpdateCredential(ctx context.Context, credID int, value string) (*domain.Credential, error) {
	if err := c.requireTypes(); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/3/people/credentials/%d", credID)
	var out credential
	if err := c.do(ctx, "PUT", path, &valueUpdate{Value: value}, &out); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.status == http.StatusConflict && ae.inUse {
			return nil, &domain.InUseError{Detail: ae.body}
		}
		return nil, err
	}
	return &domain.Credential{ID: out.ID, Value: out.Value}, nil
}

func (c *Client) DeleteCredential(ctx context.Context, credID int) error {
	path := fmt.Sprintf("/api/3/people/credentials/%d", credID)
	if err := c.do(ctx, "DELETE", path, nil, nil); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.status == http.StatusNotFound {
			return domain.ErrNotFound
		}
		return err
	}
	return nil
}

// Sync commits accumulated mutations to Doorflow's readers.
func (c *Client) Sync(ctx context.Context) error {
	return c.do(ctx, "POST", "/api/2/sync", nil, nil)
}
