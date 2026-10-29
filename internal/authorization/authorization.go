// Package authorization implements Curu's deny-by-default application policy.
// It has no dependency on JWTs, HTTP, or provider-specific claims.
package authorization

import (
	"errors"
	"fmt"
	"strings"
)

var ErrDenied = errors.New("authorization denied")

// Action names the bounded owner capabilities in the accepted application
// policy. Later use cases must request one of these actions explicitly.
type Action string

const (
	Read     Action = "read"
	Mutate   Action = "mutate"
	Archive  Action = "archive"
	Recovery Action = "recovery"
)

type ExternalIdentity struct {
	Issuer  string
	Subject string
}

// Principal is the internal application identity. Provider claims never enter
// this type; OwnerID is the stable identifier stored on owned records.
type Principal struct {
	OwnerID string
}

type Policy struct {
	ownerIdentity ExternalIdentity
	ownerID       string
}

func NewPolicy(ownerIdentity ExternalIdentity, ownerID string) (Policy, error) {
	if ownerIdentity.Issuer == "" || ownerIdentity.Subject == "" {
		return Policy{}, errors.New("owner identity issuer and subject are required")
	}
	normalizedOwnerID, err := NormalizeUUID(ownerID)
	if err != nil {
		return Policy{}, fmt.Errorf("owner ID: %w", err)
	}
	return Policy{ownerIdentity: ownerIdentity, ownerID: normalizedOwnerID}, nil
}

// PrincipalFor maps only the exact, case-sensitive configured external
// identity. Authentication success alone never creates application authority.
func (p Policy) PrincipalFor(identity ExternalIdentity) (Principal, error) {
	if identity != p.ownerIdentity {
		return Principal{}, ErrDenied
	}
	return Principal{OwnerID: p.ownerID}, nil
}

// Authorize enforces both the known action and the owner boundary at a use-case
// seam. HTTP authorization does not replace this check for data operations.
func (p Policy) Authorize(principal Principal, action Action, resourceOwnerID string) error {
	normalizedOwnerID, err := NormalizeUUID(resourceOwnerID)
	if err != nil || normalizedOwnerID != p.ownerID || principal.OwnerID != p.ownerID {
		return ErrDenied
	}
	switch action {
	case Read, Mutate, Archive, Recovery:
		return nil
	default:
		return ErrDenied
	}
}

// NormalizeUUID validates the canonical textual UUID form used by the schema.
func NormalizeUUID(value string) (string, error) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", errors.New("must be a canonical UUID")
	}
	for i, character := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return "", errors.New("must be a canonical UUID")
		}
	}
	return strings.ToLower(value), nil
}
