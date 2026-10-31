// Package auth0test is a local stand-in for an Auth0 tenant, so tests can authenticate with real RS256 tokens
// without reaching Auth0.
//
// Typical use:
//
//	issuer := auth0test.NewIssuer(t)
//	validate, err := router.NewAuth0Validator(issuer.URL, audience)
//	token := auth0test.Sign(t, issuer.Key, issuer.Claims(subject, audience))
//	req.Header.Set("Authorization", "Bearer auth0|"+token)
package auth0test

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// rs256 is the algorithm Auth0 signs access tokens with.
const rs256 = "RS256"

// keyID is the key ID of the issuer's signing key, which Header puts in every token's header.
const keyID = "test-key"

// Issuer serves the OpenID discovery document and a JWKS holding the public half of Key, from a local HTTP
// server that is closed when the test finishes.
type Issuer struct {
	// URL is the issuer URL, ending in "/" as Auth0's does.
	URL *url.URL
	Key *rsa.PrivateKey
}

// NewIssuer starts an Issuer with a new 2048-bit RSA signing key.
func NewIssuer(t testing.TB) Issuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating signing key: %v", err)
	}

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]string{"jwks_uri": server.URL + "/.well-known/jwks.json"})
	})
	mux.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA",
			"use": "sig",
			"alg": rs256,
			"kid": keyID,
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})

	issuerURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("parsing issuer URL: %v", err)
	}

	return Issuer{URL: issuerURL, Key: key}
}

func writeJSON(t testing.TB, w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("writing issuer response: %v", err)
	}
}

// Claims returns the claims of a token for subject that a validator for the issuer and audience accepts: it
// was issued now and expires in an hour.
func (i Issuer) Claims(subject, audience string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": i.URL.String(),
		"sub": subject,
		"aud": []string{audience},
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
}

// Header returns the JWS header Sign uses: RS256 under the issuer's key ID. Tests change it and pass it to
// SignWithHeader to build tokens a validator must reject.
func Header() map[string]any {
	return map[string]any{"alg": rs256, "typ": "JWT", "kid": keyID}
}

// Sign returns a compact RS256 JWS of claims, signed with key under the issuer's key ID.
func Sign(t testing.TB, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()

	return SignWithHeader(t, key, Header(), claims)
}

// SignWithHeader returns a compact JWS of claims under header, whatever the header says. The signature is made
// by header's "alg" with key: "RS256" needs an *rsa.PrivateKey, "HS256" a []byte secret, and "none" ignores key
// and leaves the signature empty.
func SignWithHeader(t testing.TB, key any, header, claims map[string]any) string {
	t.Helper()

	encode := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("encoding token: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}

	signingInput := encode(header) + "." + encode(claims)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature(t, key, header["alg"], signingInput))
}

func signature(t testing.TB, key, alg any, signingInput string) []byte {
	t.Helper()

	switch alg {
	case "none":
		return nil
	case rs256:
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			t.Fatalf("RS256 needs an *rsa.PrivateKey, got %T", key)
		}
		digest := sha256.Sum256([]byte(signingInput))
		sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatalf("signing token: %v", err)
		}
		return sig
	case "HS256":
		secret, ok := key.([]byte)
		if !ok {
			t.Fatalf("HS256 needs a []byte secret, got %T", key)
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(signingInput))
		return mac.Sum(nil)
	default:
		t.Fatalf("unsupported signing algorithm %v", alg)
		return nil
	}
}
