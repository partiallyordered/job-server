// Package internal logically splits some supporting functionality from the server predominantly in
// order to support testing.
package internal

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// validateCreds returns an error for server certs and client CA certs that do not use Ed25519 keys.
func validateCreds(serverCert *tls.Certificate, clientCaCert *x509.Certificate) error {
	if _, ok := serverCert.PrivateKey.(ed25519.PrivateKey); !ok {
		return errors.New("server private key must be Ed25519")
	}
	if _, ok := clientCaCert.PublicKey.(ed25519.PublicKey); !ok {
		return errors.New("client CA cert must be signed with an Ed25519 key")
	}
	return nil
}

// acceptOnlyed25519 is used as the verifyPeerCertificate property when we create a [tls.Config].
// It rejects client certificates not using an Ed25519 key.
func acceptOnlyEd25519ClientCertKey(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("parsing client certificate: %w", err)
	}
	if _, ok := cert.PublicKey.(ed25519.PublicKey); !ok {
		return errors.New("client certificate must use an Ed25519 key")
	}
	return nil
}

// LoadCreds loads the server certificate, private key and client CA cert for mTLS. It will return
// an error if
// - it cannot read the files containing these credentials, or
// - it cannot decode them, or
// - any non-Ed25519 keys are used
func LoadCreds(
	serverCertPath string,
	serverKeyPath string,
	clientCACertPath string,
) (credentials.TransportCredentials, error) {
	serverCert, err := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
	if err != nil {
		return nil, fmt.Errorf("loading server cert/key: %w", err)
	}

	clientCaCertPem, err := os.ReadFile(clientCACertPath)
	if err != nil {
		return nil, fmt.Errorf("reading client CA cert: %w", err)
	}

	// In order to ensure a secure configuration we accept only Ed25519-signed certs
	// TODO: here, we only read the first PEM block in the input data. Should read all, but we know
	// our input contains only one.
	block, _ := pem.Decode(clientCaCertPem)
	if block == nil {
		return nil, errors.New("decoding client CA cert PEM")
	}
	clientCaCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing client CA cert: %w", err)
	}

	if err := validateCreds(&serverCert, clientCaCert); err != nil {
		return nil, fmt.Errorf("validating credentials: %w", err)
	}

	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(clientCaCert)

	return credentials.NewTLS(&tls.Config{
		Certificates:          []tls.Certificate{serverCert},
		ClientAuth:            tls.RequireAndVerifyClientCert,
		ClientCAs:             clientCAs,
		MinVersion:            tls.VersionTLS13,
		VerifyPeerCertificate: acceptOnlyEd25519ClientCertKey,
	}), nil
}
