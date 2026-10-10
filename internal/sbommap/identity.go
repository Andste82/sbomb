package sbommap

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
)

// documentNamespace is the UUID namespace of every reproducible document
// identity sbomb derives. It is the RFC 4122 URL namespace, kept from the
// first serial number sbomb wrote so that no reproducible CycloneDX serial
// moved when the derivation was shared.
const documentNamespace = "6ba7b811-9dad-11d1-80b4-00c04fd430c8"

// DocumentUUID derives the identity of a reproducible document from the
// canonical encoding of what it states: a version 5 UUID over the SHA-256 of
// that encoding (section 29). The same statement gives the same identity, a
// different one gives a different identity, and nothing volatile may be in the
// input -- which is the caller's to guarantee, because only the caller knows
// which of its fields are.
//
// It returns the bare UUID. CycloneDX writes it as a urn:uuid serial number
// and SPDX as the namespace of its element IRIs; neither spelling belongs
// here.
func DocumentUUID(digestInput []byte) string {
	hash := sha256.Sum256(digestInput)
	name := "sbomb:" + hex.EncodeToString(hash[:])
	namespace := uuid.MustParse(documentNamespace)
	return uuid.NewSHA1(namespace, []byte(name)).String()
}
