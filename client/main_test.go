package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"math/rand/v2"
	"net"
	"os"
	"testing"
	"time"

	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/partiallyordered/int-backend-matt-1/internal"
	"github.com/partiallyordered/int-backend-matt-1/server/jobserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/test/bufconn"
)

func TestClientRejectsNonEd25519ServerCert(t *testing.T) {
	// Load server CA key to sign a server cert
	caKeyPEM, err := os.ReadFile("../server/server-ca.key")
	require.NoError(t, err)
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	require.NotNil(t, caKeyBlock)
	caKeyIface, err := x509.ParsePKCS8PrivateKey(caKeyBlock.Bytes)
	require.NoError(t, err)
	caKey, ok := caKeyIface.(ed25519.PrivateKey)
	require.True(t, ok)

	// Load server CA cert
	caCertPEM, err := os.ReadFile("server-ca.crt")
	require.NoError(t, err)
	caCertBlock, _ := pem.Decode(caCertPEM)
	require.NotNil(t, caCertBlock)
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	require.NoError(t, err)

	// Generate a server cert with an ECDSA key, signed by the real server CA
	rng := rand.NewChaCha8([32]byte{42})
	ecKey, err := ecdsa.GenerateKey(elliptic.P384(), rng)
	require.NoError(t, err)
	serverCertDER, err := x509.CreateCertificate(
		rng,
		&x509.Certificate{
			SerialNumber: big.NewInt(1),
			DNSNames:     []string{"localhost"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
		},
		caCert,
		ecKey.Public(),
		caKey,
	)
	require.NoError(t, err)

	// Load client CA cert
	clientCaCertPEM, err := os.ReadFile("../server/client-ca.crt")
	require.NoError(t, err)
	clientCaCertBlock, _ := pem.Decode(clientCaCertPEM)
	require.NotNil(t, clientCaCertBlock)
	clientCaCert, err := x509.ParseCertificate(clientCaCertBlock.Bytes)
	require.NoError(t, err)
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(clientCaCert)

	// Start a server presenting the non-Ed25519 cert
	const bufSize = 1024 * 1024
	lis := bufconn.Listen(bufSize)
	allowlist, err := internal.GenerateAllowList()
	require.NoError(t, err)
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{serverCertDER},
			PrivateKey:  ecKey,
		}},
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  clientCAs,
		MinVersion: tls.VersionTLS13,
		ServerName: "localhost",
	})))
	pb.RegisterJobServiceServer(s, jobserver.Create(allowlist))
	go func() {
		if err := s.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			assert.NoError(t, err)
		}
	}()
	t.Cleanup(s.Stop)

	// Use the actual client credentials from loadCreds()
	creds, err := loadCreds()
	require.NoError(t, err)

	grpcConn, err := grpc.NewClient(
		"localhost:443",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(creds),
	)
	require.NoError(t, err)
	defer grpcConn.Close()

	_, err = pb.NewJobServiceClient(grpcConn).GetJobStatus(t.Context(), new(pb.GetJobStatusRequest))
	assert.ErrorContains(t, err, "certificate must use an Ed25519 key")
}
