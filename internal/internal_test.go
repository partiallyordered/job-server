package internal

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCredsRejectsRSAServerKey(t *testing.T) {
	rng := rand.NewChaCha8([32]byte{42})
	rsaKey, err := rsa.GenerateKey(rng, 4096)
	require.NoError(t, err)
	ed25519Pub, _, err := ed25519.GenerateKey(rng)
	require.NoError(t, err)
	err = validateCreds(
		&tls.Certificate{PrivateKey: rsaKey},
		&x509.Certificate{PublicKey: ed25519Pub},
	)
	assert.ErrorContains(t, err, "server private key must be Ed25519")
}

func TestValidateCredsRejectsECDSAClientCACert(t *testing.T) {
	rng := rand.NewChaCha8([32]byte{42})
	_, ed25519Key, err := ed25519.GenerateKey(rng)
	require.NoError(t, err)
	ecKey, err := ecdsa.GenerateKey(elliptic.P384(), rng)
	require.NoError(t, err)
	err = validateCreds(
		&tls.Certificate{PrivateKey: ed25519Key},
		&x509.Certificate{PublicKey: ecKey.Public()},
	)
	assert.ErrorContains(t, err, "client CA cert must be signed with an Ed25519 key")
}

func TestLoadCreds(t *testing.T) {
	_, err := LoadCreds("../server/server.crt", "../server/server.key", "../server/client-ca.crt")
	assert.NoError(t, err)
}
