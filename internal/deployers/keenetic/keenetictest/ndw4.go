// Package keenetictest models the device side of the KeeneticOS 5.2
// x-ndw4-interactive login for test doubles. It is written from the device's
// point of view, verifying proofs against a stored key, and shares no code with
// the deployer, so a double built on it checks the client instead of repeating
// it. Only tests import it.
package keenetictest

import (
	"crypto/hmac"
	"crypto/sha3"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Challenge is the WWW-Authenticate value a 5.2 device sends for this login.
const Challenge = `x-ndw4-interactive endpoint="/auth" data="e30="`

// NDW4 is one device account and the exchanges in flight for it.
type NDW4 struct {
	Login      string
	Password   string
	Salt       []byte
	Iterations uint32
	MemoryKiB  uint32
	// ForgeSignature signs the second answer with a key derived from another
	// password: a device that accepts the proof without knowing the password.
	ForgeSignature bool

	mu        sync.Mutex
	exchanges int
	pending   map[string]string
	signed    map[string]string
}

// Owns reports whether a POST /auth body belongs to this exchange rather than
// to the older login, which carries a password field instead of a nonce.
func Owns(body []byte) bool {
	var fields map[string]any
	if json.Unmarshal(body, &fields) != nil {
		return false
	}
	_, nonce := fields["nonce"]
	return nonce
}

// Answer handles one POST /auth body of the exchange. It returns the status, the
// X-NDM-Data value to send (empty for none) and whether the login completed.
func (d *NDW4) Answer(body []byte) (int, string, bool) {
	var request map[string]string
	if json.Unmarshal(body, &request) != nil || request["login"] != d.Login || request["nonce"] == "" {
		return http.StatusUnauthorized, "", false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending == nil {
		d.pending, d.signed = map[string]string{}, map[string]string{}
	}
	nonce := request["nonce"]
	stored, server := d.keys(d.Password)
	switch {
	case request["proof"] != "":
		message, ok := d.pending[nonce]
		delete(d.pending, nonce)
		if !ok || !provesClientKey(stored, message, request["proof"]) {
			return http.StatusUnauthorized, "", false
		}
		if d.ForgeSignature {
			_, server = d.keys(d.Password + "-other")
		}
		signature := base64.StdEncoding.EncodeToString(mac(server, message))
		d.signed[nonce] = message + ";signature4=" + signature
		return http.StatusUnauthorized, encode(map[string]any{"signature": signature}), false
	case request["signature-proof"] != "":
		message, ok := d.signed[nonce]
		delete(d.signed, nonce)
		if !ok || !provesClientKey(stored, message, request["signature-proof"]) {
			return http.StatusUnauthorized, "", false
		}
		return http.StatusOK, "", true
	default:
		d.exchanges++
		salt := base64.StdEncoding.EncodeToString(d.Salt)
		serverNonce := fmt.Sprintf("%s@device%d", nonce, d.exchanges)
		d.pending[serverNonce] = fmt.Sprintf("login1=%s,nonce1=%s;iter2=%d,memcost2=%d,nonce2=%s,salt2=%s;login3=%s,nonce3=%s",
			d.Login, nonce, d.Iterations, d.MemoryKiB, serverNonce, salt, d.Login, serverNonce)
		return http.StatusUnauthorized, encode(map[string]any{"salt": salt, "nonce": serverNonce, "iter": d.Iterations, "memcost": d.MemoryKiB}), false
	}
}

// keys are what the device holds for an account: the stored key it verifies
// proofs against and the key it signs with. Neither recovers the password.
func (d *NDW4) keys(password string) (stored, server []byte) {
	derived := argon2.IDKey([]byte(password), d.Salt, d.Iterations, d.MemoryKiB, 1, 64)
	client := mac(derived, "NDW4 Interactive Client Key")
	sum := sha3.Sum512(client)
	return sum[:], mac(derived, "NDW4 Interactive Server Key")
}

// provesClientKey recovers the client key from a proof and checks it against
// the stored key, which is how a device verifies without the password.
func provesClientKey(stored []byte, message, proof string) bool {
	decoded, err := base64.StdEncoding.DecodeString(proof)
	signature := mac(stored, message)
	if err != nil || len(decoded) != len(signature) {
		return false
	}
	for index := range decoded {
		decoded[index] ^= signature[index]
	}
	recovered := sha3.Sum512(decoded)
	return hmac.Equal(recovered[:], stored)
}

func mac(key []byte, message string) []byte {
	signer := hmac.New(func() hash.Hash { return sha3.New512() }, key)
	_, _ = signer.Write([]byte(message))
	return signer.Sum(nil)
}

func encode(value any) string {
	payload, _ := json.Marshal(value)
	return base64.StdEncoding.EncodeToString(payload)
}
