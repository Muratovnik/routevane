package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

func (s *PublicationService) Subscription(ctx context.Context, token string) (ArtifactPayload, error) {
	tokenID, hash, ok := parseToken(token)
	if !ok {
		return ArtifactPayload{}, ErrNotFound
	}
	output, err := s.config.Store.OutputBySubscription(ctx, tokenID, hash)
	if err != nil {
		return ArtifactPayload{}, err
	}
	if output.LatestArtifactID != "" {
		artifact, readErr := s.verifiedArtifact(ctx, output.LatestArtifactID)
		if readErr == nil {
			return artifact, nil
		}
	}
	if output.PreviousArtifactID != "" {
		artifact, readErr := s.verifiedArtifact(ctx, output.PreviousArtifactID)
		if readErr == nil {
			artifact.Fallback = true
			return artifact, nil
		}
	}
	return ArtifactPayload{}, ErrArtifactUnavailable
}

// issueSubscriptionToken mints one bearer token, extracted out of AddOutput:
// a token identity, a secret, the token string itself, and the hash the store
// keeps in place of it.
func issueSubscriptionToken(entropy io.Reader) (tokenID string, token string, hash [32]byte, err error) {
	tokenID, err = randomHex(entropy, 16)
	if err != nil {
		return "", "", hash, fmt.Errorf("generate subscription identity: %w", err)
	}
	secret := make([]byte, 32)
	if _, err := io.ReadFull(entropy, secret); err != nil {
		return "", "", hash, fmt.Errorf("generate subscription secret: %w", err)
	}
	token = "rv1." + tokenID + "." + base64.RawURLEncoding.EncodeToString(secret)
	hash = sha256.Sum256([]byte(token))
	return tokenID, token, hash, nil
}

func parseToken(token string) (string, [32]byte, bool) {
	var zero [32]byte
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "rv1" || !isHexID(parts[1]) {
		return "", zero, false
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(secret) != 32 {
		return "", zero, false
	}
	return parts[1], sha256.Sum256([]byte(token)), true
}
