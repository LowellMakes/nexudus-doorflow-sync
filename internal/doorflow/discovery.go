package doorflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// ResolveTypes discovers the account's credential_type_id for pin / keyfob /
// card from the credential_types endpoint and caches them. Credential writes are
// gated on this: until every kind maps to a non-zero id, credential writes fail
// with a clear error (task 2.6).
func (c *Client) ResolveTypes(ctx context.Context) error {
	var list credentialTypeList
	if err := c.get(ctx, "/api/3/credential_types", &list, nil); err != nil {
		return err
	}
	kindFor := map[string]domain.CredentialKind{
		"pin":    domain.KindPin,
		"keyfob": domain.KindKeyFob,
		"card":   domain.KindCard,
	}

	c.mu.Lock()
	kindToID := map[domain.CredentialKind]int{}
	idToKind := map[int]domain.CredentialKind{}
	for _, ct := range list.Result {
		if k, ok := kindFor[strings.ToLower(ct.Slug)]; ok {
			kindToID[k] = ct.ID
			idToKind[ct.ID] = k
		}
	}
	// require all three to be present and non-zero before enabling writes
	for _, k := range []domain.CredentialKind{domain.KindPin, domain.KindKeyFob, domain.KindCard} {
		id, ok := kindToID[k]
		if !ok || id == 0 {
			c.mu.Unlock()
			return fmt.Errorf("doorflow: credential type for %q not found in credential_types", k)
		}
	}
	c.kindToID = kindToID
	c.idToKind = idToKind
	c.typesReady = true
	c.mu.Unlock()

	resolved := map[domain.CredentialKind]int{
		domain.KindPin:    kindToID[domain.KindPin],
		domain.KindKeyFob: kindToID[domain.KindKeyFob],
		domain.KindCard:   kindToID[domain.KindCard],
	}
	c.log.Log(ctx, slog.LevelInfo, "credential types resolved", "resolved", resolved)
	return nil
}

// TypesReady reports whether credential types are resolved (writes gated on it).
func (c *Client) TypesReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.typesReady
}

func (c *Client) typeID(kind domain.CredentialKind) (int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.kindToID[kind]
	if !ok || id == 0 {
		return 0, fmt.Errorf("doorflow: no resolved credential type for %q", kind)
	}
	return id, nil
}

func (c *Client) requireTypes() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.typesReady {
		return errors.New("doorflow: credential types not resolved; call ResolveTypes before credential writes")
	}
	return nil
}
