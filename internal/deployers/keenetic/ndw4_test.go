package keenetic

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/deployers/keenetic/keenetictest"
)

// The expected values come from Node's OpenSSL-backed argon2id, HMAC and
// SHA3-512, following the algorithm in the KeeneticOS 5.2 web interface. They
// were not produced by this package, so a shared mistake cannot agree with
// itself here (ADR 0058).
func TestNDW4ExchangeMatchesAnIndependentImplementation(t *testing.T) {
	const (
		clientNonce = "AAECAwQFBgcICQoLDA0ODw=="
		serverNonce = clientNonce + "@3948e13db8154ccf%cCHMdfnB"
	)
	data := base64.StdEncoding.EncodeToString([]byte(`{"salt":"8OHSw7Sllod4aVpLPC0eDw==","nonce":"` + serverNonce + `","iter":2,"memcost":8}`))
	parameters, err := parseNDW4Parameters(data)
	if err != nil {
		t.Fatal(err)
	}
	keys := deriveNDW4Keys(devicePassword, parameters)
	message := ndw4AuthMessage(deviceUser, clientNonce, parameters)
	signature := base64.StdEncoding.EncodeToString(ndw4DeviceSignature(keys, message))
	for name, got := range map[string]string{
		"client key":      hex.EncodeToString(keys.client),
		"stored key":      hex.EncodeToString(keys.stored),
		"server key":      hex.EncodeToString(keys.server),
		"message":         message,
		"proof":           base64.StdEncoding.EncodeToString(ndw4Proof(keys, message)),
		"signature":       signature,
		"signature proof": base64.StdEncoding.EncodeToString(ndw4SignatureProof(keys, message, signature)),
	} {
		want := map[string]string{
			"client key":      "8c26f50cd23ec452086491266310e0878f0dd515a45a242025d79acfffb643fc096654b8d5824d8d468a35528061b3607523dde4b531ed13a4f00ef944ea3999",
			"stored key":      "1a2f66d76dde580574d747c001c49b34f07a62fdef0c354a5c29cb1dfe89663448866fffc0ddcb6f3c03df344eea49000828c151f445a5be152375a3f40f1789",
			"server key":      "6f7efe78ce89e36254b8e8fd954e5a4801bc026446af6361b2084595e1181bf0c52fb4c0fa2132cc5a6ff416c7e6d4bcf4bbdc9ffd93d7c6bda60d0f7053d2d8",
			"message":         "login1=admin,nonce1=" + clientNonce + ";iter2=2,memcost2=8,nonce2=" + serverNonce + ",salt2=8OHSw7Sllod4aVpLPC0eDw==;login3=admin,nonce3=" + serverNonce,
			"proof":           "3ArbRk+HABLbcDXSQwDQ8vYdrIow2YYT2zZ43Q5mJ2gaSku0J+inPcqtkQU9WT5gTAzIz4RhMoa4Bh7O8DD5ow==",
			"signature":       "HhwHyJ/iD7Dat2HvbpLYRM2Z+A0PWn56Kjx9BbvM2/TCoRkfAQR6VzNj/rW834xqjl7xxrNgXruLWBf8meriJw==",
			"signature proof": "272Y1x/1EPtcu87bEyISBs0PQbVOUvkmgVZ2VWjViMZbdZnvvWyCVT4dkYrKjZadMjSKwqLFVnW+XMlLlNdXxQ==",
		}[name]
		if got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
}

func TestLoginPrefersNDW4AndNeverRetriesAfterTheProof(t *testing.T) {
	salt := []byte("0123456789abcdef")
	for _, test := range []struct {
		name            string
		changedPassword bool
		forgeSignature  bool
		emptyNDW4Answer bool
		refuseNDW2      bool
		wantErr         error
		wantKind        error
		wantNDW2        int
		wantNDW4        int
	}{
		// The 5.2 router of ADR 0058 refuses the older login for this account.
		{name: "5.2 device", refuseNDW2: true, wantNDW4: 3},
		{name: "wrong password", changedPassword: true, refuseNDW2: true, wantErr: ErrDeviceRefused, wantKind: application.ErrDeviceAuthRejected, wantNDW4: 2},
		{name: "device without the password", forgeSignature: true, wantErr: ErrDeviceAnswer, wantKind: application.ErrDeviceAnswerInvalid, wantNDW4: 2},
		{name: "unusable first answer", emptyNDW4Answer: true, wantNDW2: 1, wantNDW4: 1},
		{name: "unusable first answer and refused older login", emptyNDW4Answer: true, refuseNDW2: true, wantErr: ErrDeviceRefused, wantKind: application.ErrDeviceAuthRejected, wantNDW2: 1, wantNDW4: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := devicePassword
			if test.changedPassword {
				account += "-changed"
			}
			double := newDeviceDouble("5.2.0")
			double.ndw4 = &keenetictest.NDW4{Login: deviceUser, Password: account, Salt: salt, Iterations: 2, MemoryKiB: 8, ForgeSignature: test.forgeSignature}
			double.emptyNDW4Answer = test.emptyNDW4Answer
			double.refuseNDW2 = test.refuseNDW2
			server := httptest.NewServer(double.handler())
			defer server.Close()
			deployer := NewFQDNDeployer(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}})
			connection := application.Connection{URL: "http://10.255.0.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface}

			info, err := deployer.Probe(context.Background(), connection)
			if test.wantErr == nil && (err != nil || info.FormatKey == "") || test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("probe = %#v, %v; want error %v", info, err, test.wantErr)
			}
			if test.wantKind != nil && !errors.Is(err, test.wantKind) {
				t.Fatalf("probe = %v; want reason %v", err, test.wantKind)
			}
			if test.wantKind != application.ErrDeviceAuthRejected && errors.Is(err, application.ErrDeviceAuthRejected) {
				t.Fatalf("probe = %v reads as a rejected password", err)
			}
			if ndw2, ndw4 := double.logins(); ndw2 != test.wantNDW2 || ndw4 != test.wantNDW4 {
				t.Fatalf("logins: older=%d newer=%d, want %d and %d", ndw2, ndw4, test.wantNDW2, test.wantNDW4)
			}
			if err != nil && strings.Contains(err.Error(), devicePassword) {
				t.Fatalf("the error carries the password: %v", err)
			}
		})
	}
}

func TestNDW4RefusesHostileParametersBeforeDerivingAKey(t *testing.T) {
	encode := func(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }
	valid := `{"salt":"MDEyMzQ1Njc4OWFiY2RlZg==","nonce":"abc@1","iter":2,"memcost":8}`
	if _, err := parseNDW4Parameters(encode(valid)); err != nil {
		t.Fatalf("the device's own answer was refused: %v", err)
	}
	for name, data := range map[string]string{
		"absent":             "",
		"not base64":         "{{{",
		"not an object":      encode(`[1,2]`),
		"beyond the bound":   encode(`{"pad":"` + strings.Repeat("a", ndw4MaxDataBytes) + `"}`),
		"short salt":         encode(strings.Replace(valid, "MDEyMzQ1Njc4OWFiY2RlZg==", "MDEyMzQ1Njc4OWFiY2Rl", 1)),
		"no nonce":           encode(strings.Replace(valid, `"abc@1"`, `""`, 1)),
		"nonce separator":    encode(strings.Replace(valid, `"abc@1"`, `"abc,login1=x"`, 1)),
		"nonce control":      encode(strings.Replace(valid, `"abc@1"`, `"abc\n1"`, 1)),
		"no iterations":      encode(strings.Replace(valid, `"iter":2`, `"iter":0`, 1)),
		"too many passes":    encode(strings.Replace(valid, `"iter":2`, `"iter":11`, 1)),
		"fractional passes":  encode(strings.Replace(valid, `"iter":2`, `"iter":2.0`, 1)),
		"exponent passes":    encode(strings.Replace(valid, `"iter":2`, `"iter":2e0`, 1)),
		"quoted passes":      encode(strings.Replace(valid, `"iter":2`, `"iter":"2"`, 1)),
		"too little memory":  encode(strings.Replace(valid, `"memcost":8`, `"memcost":7`, 1)),
		"too much memory":    encode(strings.Replace(valid, `"memcost":8`, `"memcost":65537`, 1)),
		"negative memory":    encode(strings.Replace(valid, `"memcost":8`, `"memcost":-8`, 1)),
		"overflowing memory": encode(strings.Replace(valid, `"memcost":8`, `"memcost":4294967304`, 1)),
	} {
		if _, err := parseNDW4Parameters(data); !errors.Is(err, ErrDeviceAnswer) {
			t.Errorf("%s: accepted or misclassified: %v", name, err)
		}
	}
}

func TestNDW4EndpointStaysOnTheDevice(t *testing.T) {
	for _, test := range []struct {
		challenges []string
		endpoint   string
		offered    bool
		refused    bool
	}{
		{challenges: []string{`x-ndw2-interactive realm="Keenetic" challenge="abc"`, keenetictest.Challenge}, endpoint: "/auth", offered: true},
		{challenges: []string{`X-NDW4-Interactive endpoint="/auth/v4"`}, endpoint: "/auth/v4", offered: true},
		{challenges: []string{`x-ndw2-interactive realm="Keenetic", x-ndw4-interactive endpoint="/auth"`}, endpoint: "/auth", offered: true},
		{challenges: []string{`x-ndw4-interactive data="e30="`}, endpoint: "/auth", offered: true},
		{challenges: []string{`x-ndw2-interactive realm="Keenetic" challenge="abc"`}},
		{challenges: []string{`xx-ndw4-interactive endpoint="/auth"`}},
		{challenges: []string{`x-ndw4-interactive endpoint="//example.com/auth"`}, offered: true, refused: true},
		{challenges: []string{`x-ndw4-interactive endpoint="http://example.com/auth"`}, offered: true, refused: true},
		{challenges: []string{`x-ndw4-interactive endpoint="/auth?next=x"`}, offered: true, refused: true},
		{challenges: []string{`x-ndw4-interactive endpoint="/\\example.com"`}, offered: true, refused: true},
	} {
		endpoint, offered, err := ndw4Endpoint(test.challenges)
		if endpoint != test.endpoint || offered != test.offered || (err != nil) != test.refused || test.refused && !errors.Is(err, errNDW4Unavailable) {
			t.Errorf("%q = %q %t %v", test.challenges, endpoint, offered, err)
		}
	}
}

// A device that renews its session cookie during the login must receive only
// the renewed value afterwards; a jar that kept both would send the stale one
// first.
func TestSessionSendsOnlyTheLatestCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/auth" && r.Method == http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "challenge"})
			http.SetCookie(w, &http.Cookie{Name: "obsolete", Value: "x"})
			w.Header().Set("X-NDM-Realm", deviceRealm)
			w.Header().Set("X-NDM-Challenge", deviceChallenge)
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/auth":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "authenticated"})
			http.SetCookie(w, &http.Cookie{Name: "obsolete", MaxAge: -1})
			w.WriteHeader(http.StatusOK)
		case r.Header.Get("Cookie") == "session=authenticated":
			writeJSON(w, map[string]any{"release": "5.1.2"})
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	deployer := New(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}})
	session, err := deployer.connect(context.Background(), application.Connection{URL: "http://10.255.0.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface})
	if err != nil {
		t.Fatal(err)
	}
	defer session.close()
	var version map[string]any
	if err := session.command(context.Background(), "/rci/show/version", nil, &version); err != nil {
		t.Fatalf("the renewed session cookie was not the one sent: %v", err)
	}
}

func FuzzNDW4Parameters(f *testing.F) {
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"salt":"MDEyMzQ1Njc4OWFiY2RlZg==","nonce":"abc@1","iter":2,"memcost":8}`)))
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"salt":"","nonce":"","iter":1e308,"memcost":-0}`)))
	f.Add("e30=")
	f.Fuzz(func(t *testing.T, data string) {
		parameters, err := parseNDW4Parameters(data)
		if err != nil {
			return
		}
		if len(parameters.salt) != ndw4SaltBytes || !validNDW4Nonce(parameters.nonce) ||
			parameters.iterations < 1 || parameters.iterations > ndw4MaxIterations ||
			parameters.memoryKiB < ndw4MinMemoryKiB || parameters.memoryKiB > ndw4MaxMemoryKiB {
			t.Fatalf("accepted out-of-bound parameters %#v", parameters)
		}
		if value, ok := ndw4Factor(json.RawMessage(parameters.iterText), 1, ndw4MaxIterations); !ok || value != parameters.iterations {
			t.Fatalf("signed iteration text %q does not state %d", parameters.iterText, parameters.iterations)
		}
	})
}
