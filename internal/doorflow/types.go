package doorflow

import "github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"

// The Doorflow API field names below follow the documented v2 (person) and v3
// (credential) shapes. Exact naming is one of the tracked Open Questions; this
// is the contract we build against and the tests assert the round-trip.

// personReq/resp is the person resource.
type personPayload struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Enabled   bool   `json:"enabled"`
	GroupIDs  []int  `json:"group_ids"`
	SystemID  string `json:"system_id"`
}

type person struct {
	ID int `json:"id"`
	personPayload
}

// credentialReq is the create payload for a dedicated credential of a type.
type credentialReq struct {
	CredentialTypeID int    `json:"credential_type_id"`
	Value            string `json:"value"`
}

// credential is the credential resource as returned by the API (value is
// present on read, which is how we capture an auto-generated PIN).
type credential struct {
	ID               int    `json:"id"`
	CredentialTypeID int    `json:"credential_type_id"`
	Value            string `json:"value"`
}

// credentialType is an account credential type (slug pin/keyfob/card).
type credentialType struct {
	ID   int    `json:"id"`
	Slug string `json:"slug"`
}

type credentialList struct {
	Result []credential `json:"result"`
}

type credentialTypeList struct {
	Result []credentialType `json:"result"`
}

// fromDomainPerson converts our domain person to the wire payload.
func fromDomainPerson(p *domain.Person) *personPayload {
	g := make([]int, 0, len(p.GroupIDs))
	for _, id := range p.GroupIDs {
		g = append(g, int(id))
	}
	return &personPayload{
		Email:     p.Email,
		FirstName: p.FirstName,
		LastName:  p.LastName,
		Enabled:   p.Enabled,
		GroupIDs:  g,
		SystemID:  p.SystemID,
	}
}

// toDomainPerson converts a wire person to the domain projection.
func toDomainPerson(p *person) *domain.Person {
	g := make([]int32, 0, len(p.GroupIDs))
	for _, id := range p.GroupIDs {
		g = append(g, int32(id))
	}
	return &domain.Person{
		ID:        p.ID,
		Email:     p.Email,
		FirstName: p.FirstName,
		LastName:  p.LastName,
		Enabled:   p.Enabled,
		GroupIDs:  g,
		SystemID:  p.SystemID,
	}
}
