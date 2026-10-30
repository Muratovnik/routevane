package keenetic

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha3"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/Muratovnik/routevane/internal/application"
)

// The x-ndw4-interactive login KeeneticOS 5.2 introduced is a three-phase
// mutual proof: the device names a salt and work factors, the client proves a
// key derived from the password without sending it, and the device proves it
// holds the same key before the client completes the login. The exchange and
// its bounds are reproduced from the device's own web interface (ADR 0058).
const (
	ndw4Scheme         = "x-ndw4-interactive"
	ndw4ClientKeyLabel = "NDW4 Interactive Client Key"
	ndw4ServerKeyLabel = "NDW4 Interactive Server Key"
	ndw4DataHeader     = "X-NDM-Data"
	ndw4NonceBytes     = 16
	ndw4SaltBytes      = 16
	ndw4KeyBytes       = 64
	// The device's own values are 2 passes over 8 KiB. The ceilings keep a
	// hostile answer from spending this computer's memory and from outlasting
	// the device session cookie, which lives 20 seconds from the challenge.
	ndw4MaxIterations   = 10
	ndw4MinMemoryKiB    = 8
	ndw4MaxMemoryKiB    = 64 << 10
	ndw4MaxDataBytes    = 4 << 10
	ndw4MaxNonceBytes   = 256
	ndw4MaxEndpointPath = 128
)

// errNDW4Unavailable marks an exchange that stopped before anything derived
// from the password was sent, so the older login may still be tried.
var errNDW4Unavailable = errors.New("x-ndw4-interactive exchange is unavailable")

var ndw4EndpointParameter = regexp.MustCompile(`(?i)endpoint\s*=\s*"([^"]*)"`)

// ndw4Endpoint reports whether a challenge offers x-ndw4-interactive and the
// same-origin path the exchange is posted to. An endpoint that would leave the
// device's origin is refused rather than followed.
func ndw4Endpoint(challenges []string) (string, bool, error) {
	for _, challenge := range challenges {
		index := strings.Index(strings.ToLower(challenge), ndw4Scheme)
		if index < 0 || index > 0 && !strings.ContainsRune(" ,", rune(challenge[index-1])) {
			continue
		}
		match := ndw4EndpointParameter.FindStringSubmatch(challenge[index+len(ndw4Scheme):])
		if match == nil {
			return "/auth", true, nil
		}
		endpoint := match[1]
		if len(endpoint) > ndw4MaxEndpointPath || !strings.HasPrefix(endpoint, "/") || strings.HasPrefix(endpoint, "//") || strings.ContainsAny(endpoint, "\\?#@ ") {
			return "", true, fmt.Errorf("%w: %w: endpoint is not a path on the device", ErrDeviceAnswer, errNDW4Unavailable)
		}
		return endpoint, true, nil
	}
	return "", false, nil
}

// ndw4Parameters is the device's first answer. Salt, nonce and both work
// factors keep the exact text the device sent: the proof signs that text.
type ndw4Parameters struct {
	salt       []byte
	saltText   string
	nonce      string
	iterations uint32
	memoryKiB  uint32
	iterText   string
	memoryText string
}

// parseNDW4Parameters bounds the first answer before any key is derived.
func parseNDW4Parameters(encoded string) (ndw4Parameters, error) {
	var answer struct {
		Salt    string          `json:"salt"`
		Nonce   string          `json:"nonce"`
		Iter    json.RawMessage `json:"iter"`
		Memcost json.RawMessage `json:"memcost"`
	}
	if err := decodeNDW4Data(encoded, &answer); err != nil {
		return ndw4Parameters{}, err
	}
	salt, err := base64.StdEncoding.DecodeString(answer.Salt)
	if err != nil || len(salt) != ndw4SaltBytes {
		return ndw4Parameters{}, fmt.Errorf("%w: salt is not %d bytes", ErrDeviceAnswer, ndw4SaltBytes)
	}
	if !validNDW4Nonce(answer.Nonce) {
		return ndw4Parameters{}, fmt.Errorf("%w: server nonce is not usable", ErrDeviceAnswer)
	}
	iterations, ok := ndw4Factor(answer.Iter, 1, ndw4MaxIterations)
	if !ok {
		return ndw4Parameters{}, fmt.Errorf("%w: iteration count is outside 1..%d", ErrDeviceAnswer, ndw4MaxIterations)
	}
	memory, ok := ndw4Factor(answer.Memcost, ndw4MinMemoryKiB, ndw4MaxMemoryKiB)
	if !ok {
		return ndw4Parameters{}, fmt.Errorf("%w: memory cost is outside %d..%d KiB", ErrDeviceAnswer, ndw4MinMemoryKiB, ndw4MaxMemoryKiB)
	}
	return ndw4Parameters{
		salt: salt, saltText: answer.Salt, nonce: answer.Nonce,
		iterations: iterations, memoryKiB: memory,
		iterText: string(answer.Iter), memoryText: string(answer.Memcost),
	}, nil
}

// parseNDW4Signature reads the device's proof from its second answer.
func parseNDW4Signature(encoded string) (string, []byte, error) {
	var answer struct {
		Signature string `json:"signature"`
	}
	if err := decodeNDW4Data(encoded, &answer); err != nil {
		return "", nil, err
	}
	signature, err := base64.StdEncoding.DecodeString(answer.Signature)
	if err != nil || len(signature) != ndw4KeyBytes {
		return "", nil, fmt.Errorf("%w: device signature is not %d bytes", ErrDeviceAnswer, ndw4KeyBytes)
	}
	return answer.Signature, signature, nil
}

func decodeNDW4Data(encoded string, out any) error {
	if encoded == "" || len(encoded) > ndw4MaxDataBytes {
		return fmt.Errorf("%w: %s is absent or beyond its byte bound", ErrDeviceAnswer, ndw4DataHeader)
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("%w: %s is not base64", ErrDeviceAnswer, ndw4DataHeader)
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("%w: %s is not a JSON object", ErrDeviceAnswer, ndw4DataHeader)
	}
	return nil
}

// validNDW4Nonce admits printable ASCII without the separators of the signed
// message, so a nonce cannot rewrite the fields the proof covers.
func validNDW4Nonce(nonce string) bool {
	if nonce == "" || len(nonce) > ndw4MaxNonceBytes {
		return false
	}
	for _, char := range nonce {
		if char <= ' ' || char > '~' || char == ',' || char == ';' {
			return false
		}
	}
	return true
}

// ndw4Factor accepts a JSON number written as a plain decimal integer: the
// signed message carries the number as the device wrote it, which a quoted,
// fractional or exponent form would not match.
func ndw4Factor(raw json.RawMessage, minimum, maximum uint32) (uint32, bool) {
	text := string(raw)
	if text == "" || strings.Trim(text, "0123456789") != "" {
		return 0, false
	}
	value, err := strconv.ParseUint(text, 10, 32)
	if err != nil || value < uint64(minimum) || value > uint64(maximum) {
		return 0, false
	}
	return uint32(value), true
}

// ndw4Keys are the client-side keys derived from the password. Only proofs
// derived from them leave this process.
type ndw4Keys struct {
	client []byte
	stored []byte
	server []byte
}

func deriveNDW4Keys(password string, parameters ndw4Parameters) ndw4Keys {
	derived := argon2.IDKey([]byte(password), parameters.salt, parameters.iterations, parameters.memoryKiB, 1, ndw4KeyBytes)
	client := hmacSHA3(derived, ndw4ClientKeyLabel)
	stored := sha3.Sum512(client)
	return ndw4Keys{client: client, stored: stored[:], server: hmacSHA3(derived, ndw4ServerKeyLabel)}
}

// ndw4AuthMessage is the transcript both proofs sign: the client's first
// request, the device's first answer, and the client's second request.
func ndw4AuthMessage(login, clientNonce string, parameters ndw4Parameters) string {
	return "login1=" + login + ",nonce1=" + clientNonce +
		";iter2=" + parameters.iterText + ",memcost2=" + parameters.memoryText + ",nonce2=" + parameters.nonce + ",salt2=" + parameters.saltText +
		";login3=" + login + ",nonce3=" + parameters.nonce
}

func ndw4Proof(keys ndw4Keys, message string) []byte {
	return xorBytes(keys.client, hmacSHA3(keys.stored, message))
}

func ndw4DeviceSignature(keys ndw4Keys, message string) []byte {
	return hmacSHA3(keys.server, message)
}

func ndw4SignatureProof(keys ndw4Keys, message, signature string) []byte {
	return xorBytes(keys.client, hmacSHA3(keys.stored, message+";signature4="+signature))
}

func hmacSHA3(key []byte, message string) []byte {
	mac := hmac.New(func() hash.Hash { return sha3.New512() }, key)
	_, _ = mac.Write([]byte(message))
	return mac.Sum(nil)
}

func xorBytes(left, right []byte) []byte {
	out := make([]byte, len(left))
	for index := range left {
		out[index] = left[index] ^ right[index]
	}
	return out
}

// authenticateNDW4 runs the three phases. A refusal after the proof is final:
// repeating the login with another scheme would only add a failed attempt to
// the device's counter.
func (s *session) authenticateNDW4(ctx context.Context, endpoint, username, password string) error {
	nonce := make([]byte, ndw4NonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("%w: %w: client nonce", ErrDeviceRefused, errNDW4Unavailable)
	}
	clientNonce := base64.StdEncoding.EncodeToString(nonce)
	status, header, err := s.authExchange(ctx, http.MethodPost, endpoint, map[string]string{"login": username, "nonce": clientNonce})
	if err != nil {
		return err
	}
	if status != http.StatusUnauthorized {
		return fmt.Errorf("%w: %w: %w: first answer status %d", ErrDeviceRefused, application.ErrDeviceAnswerInvalid, errNDW4Unavailable, status)
	}
	parameters, err := parseNDW4Parameters(header.Get(ndw4DataHeader))
	if err != nil {
		return errors.Join(errNDW4Unavailable, err)
	}

	keys := deriveNDW4Keys(password, parameters)
	message := ndw4AuthMessage(username, clientNonce, parameters)
	status, header, err = s.authExchange(ctx, http.MethodPost, endpoint, map[string]string{
		"login": username, "nonce": parameters.nonce, "proof": base64.StdEncoding.EncodeToString(ndw4Proof(keys, message)),
	})
	if err != nil {
		return err
	}
	signatureText, signature, signatureErr := parseNDW4Signature(header.Get(ndw4DataHeader))
	if status != http.StatusUnauthorized || signatureErr != nil {
		// A device that rejects the proof answers 401 without a signature.
		return loginRefused(status)
	}
	if !hmac.Equal(ndw4DeviceSignature(keys, message), signature) {
		return fmt.Errorf("%w: the device did not prove it knows the password", ErrDeviceAnswer)
	}

	status, _, err = s.authExchange(ctx, http.MethodPost, endpoint, map[string]string{
		"login": username, "nonce": parameters.nonce, "signature-proof": base64.StdEncoding.EncodeToString(ndw4SignatureProof(keys, message, signatureText)),
	})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return loginRefused(status)
	}
	return nil
}
