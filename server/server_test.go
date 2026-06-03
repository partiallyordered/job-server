package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"math/big"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"testing"

	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

const testClientIdentity = "int-backend-matt-1-client@domain.local"

func withAllowlist(t *testing.T, l map[string][]string) {
	t.Helper()
	orig := clientAllowlist
	clientAllowlist = l
	t.Cleanup(func() { clientAllowlist = orig })
}

func startServer(t *testing.T) *bufconn.Listener {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	creds, err := loadCreds()
	require.NoError(t, err)
	s := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterJobServiceServer(s, &server{})
	go func() {
		if err := s.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			assert.NoError(t, err)
		}
	}()
	t.Cleanup(s.Stop)
	return lis
}

func newClient(t *testing.T, lis *bufconn.Listener) pb.JobServiceClient {
	t.Helper()
	clientCert, err := tls.LoadX509KeyPair("../client/client.crt", "../client/client.key")
	require.NoError(t, err)
	serverCACertPEM, err := os.ReadFile("../client/server-ca.crt")
	require.NoError(t, err)
	serverCAs := x509.NewCertPool()
	serverCAs.AppendCertsFromPEM(serverCACertPEM)
	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      serverCAs,
			ServerName:   "localhost",
			MinVersion:   tls.VersionTLS13,
		})),
	)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return pb.NewJobServiceClient(conn)
}

func TestLoadCreds(t *testing.T) {
	_, err := loadCreds()
	assert.NoError(t, err)
}

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

func TestServerRejectsNonEd25519ClientCert(t *testing.T) {
	// Retrieve/parse client CA key
	caKeyPEM, err := os.ReadFile("../client/client-ca.key")
	require.NoError(t, err)
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	require.NotNil(t, caKeyBlock)
	caKeyIface, err := x509.ParsePKCS8PrivateKey(caKeyBlock.Bytes)
	require.NoError(t, err)
	caKey, ok := caKeyIface.(ed25519.PrivateKey)
	require.True(t, ok)

	// Retrieve/parse client CA cert
	caCertPEM, err := os.ReadFile("client-ca.crt")
	require.NoError(t, err)
	caCertBlock, _ := pem.Decode(caCertPEM)
	require.NotNil(t, caCertBlock)
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	require.NoError(t, err)

	// Generate a client cert with an ECDSA key
	rng := rand.NewChaCha8([32]byte{42})
	ecKey, err := ecdsa.GenerateKey(elliptic.P384(), rng)
	require.NoError(t, err)
	clientCert, err := x509.CreateCertificate(
		rng,
		&x509.Certificate{SerialNumber: big.NewInt(1)},
		caCert,
		ecKey.Public(),
		caKey,
	)
	require.NoError(t, err)

	lis := startServer(t)

	grpcConn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{{
				Certificate: [][]byte{clientCert},
				PrivateKey:  ecKey,
			}},
			MinVersion: tls.VersionTLS13,
		})),
	)
	require.NoError(t, err)
	defer grpcConn.Close()

	_, err = pb.NewJobServiceClient(grpcConn).GetJobStatus(t.Context(), new(pb.GetJobStatusRequest))
	assert.Error(t, err)
}

func TestServerRejectsTLS12(t *testing.T) {
	lis := startServer(t)

	grpcConn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MaxVersion: tls.VersionTLS12,
		})),
	)
	require.NoError(t, err)
	defer grpcConn.Close()

	_, err = pb.NewJobServiceClient(grpcConn).GetJobStatus(t.Context(), new(pb.GetJobStatusRequest))
	assert.Error(t, err)
}

func TestResolveAllowed(t *testing.T) {
	const testIdentity = "test@example.com"

	truePath, err := exec.LookPath("true")
	require.NoError(t, err)

	t.Run("resolves bare name", func(t *testing.T) {
		withAllowlist(t, map[string][]string{testIdentity: {truePath}})
		_, err := resolveAllowed(testIdentity, "true")
		assert.NoError(t, err)
	})

	t.Run("allows absolute path", func(t *testing.T) {
		withAllowlist(t, map[string][]string{testIdentity: {truePath}})
		_, err := resolveAllowed(testIdentity, truePath)
		assert.NoError(t, err)
	})

	t.Run("rejects unallowed executable", func(t *testing.T) {
		withAllowlist(t, map[string][]string{testIdentity: {truePath}})
		_, err := resolveAllowed(testIdentity, "bash")
		assert.ErrorContains(t, err, "not permitted")
	})

	t.Run("rejects nonexistent executable", func(t *testing.T) {
		withAllowlist(t, map[string][]string{testIdentity: {}})
		_, err := resolveAllowed(testIdentity, "this-does-not-exist")
		assert.ErrorContains(t, err, "not found")
	})
}

func TestCreateJob(t *testing.T) {
	executable := "echo"
	echoPath, err := exec.LookPath(executable)
	require.NoError(t, err)
	withAllowlist(t, map[string][]string{testClientIdentity: {echoPath}})
	lis := startServer(t)
	client := newClient(t, lis)

	jobID := "test create job"
	_, err = client.CreateJob(t.Context(), pb.CreateJobRequest_builder{
		JobId:      &jobID,
		Executable: &executable,
		Args:       []string{"hello"},
	}.Build())

	assert.NoError(t, err)
}

func TestGetJobStatus(t *testing.T) {
	executable := "echo"
	echoPath, err := exec.LookPath(executable)
	require.NoError(t, err)
	withAllowlist(t, map[string][]string{testClientIdentity: {echoPath}})
	lis := startServer(t)
	client := newClient(t, lis)

	jobID := "test job status"
	_, err = client.CreateJob(t.Context(), pb.CreateJobRequest_builder{
		JobId:      &jobID,
		Executable: &executable,
		Args:       []string{"hello"},
	}.Build())
	require.NoError(t, err)

	_, err = client.GetJobStatus(t.Context(), pb.GetJobStatusRequest_builder{
		JobId: &jobID,
	}.Build())
	assert.NoError(t, err)
}

func TestStopJob(t *testing.T) {
	executable := "yes"
	yesPath, err := exec.LookPath(executable)
	require.NoError(t, err)
	withAllowlist(t, map[string][]string{testClientIdentity: {yesPath}})
	lis := startServer(t)
	client := newClient(t, lis)

	jobID := "test stop job"
	_, err = client.CreateJob(t.Context(), pb.CreateJobRequest_builder{
		JobId:      &jobID,
		Executable: &executable,
	}.Build())
	require.NoError(t, err)

	_, err = client.StopJob(t.Context(), pb.StopJobRequest_builder{
		JobId: &jobID,
	}.Build())
	assert.NoError(t, err)
}

func TestStreamJobLogEcho(t *testing.T) {
	executable := "echo"
	echoPath, err := exec.LookPath(executable)
	require.NoError(t, err)
	withAllowlist(t, map[string][]string{testClientIdentity: {echoPath}})
	lis := startServer(t)
	client := newClient(t, lis)

	jobID := "test stream echo"
	_, err = client.CreateJob(t.Context(), pb.CreateJobRequest_builder{
		JobId:      &jobID,
		Executable: &executable,
		Args:       []string{"-n", "hello"},
	}.Build())
	require.NoError(t, err)

	stream, err := client.StreamJobLog(t.Context(), pb.StreamJobLogRequest_builder{
		JobId: &jobID,
	}.Build())
	require.NoError(t, err)

	var output []byte
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		output = append(output, resp.GetOutput()...)
	}
	assert.Equal(t, []byte("hello"), output)
}

func TestStreamJobLogBinaryData(t *testing.T) {
	executable := "cat"
	catPath, err := exec.LookPath(executable)
	require.NoError(t, err)
	withAllowlist(t, map[string][]string{testClientIdentity: {catPath}})

	rng := rand.NewChaCha8([32]byte{42})
	expected := make([]byte, streamBufSize*2+1)
	_, err = rng.Read(expected)
	require.NoError(t, err)

	tmpFile := t.TempDir() + "/data.bin"
	err = os.WriteFile(tmpFile, expected, 0o600)
	require.NoError(t, err)

	lis := startServer(t)
	client := newClient(t, lis)
	jobID := "test stream binary"
	_, err = client.CreateJob(t.Context(), pb.CreateJobRequest_builder{
		JobId:      &jobID,
		Executable: &executable,
		Args:       []string{tmpFile},
	}.Build())
	require.NoError(t, err)
	stream, err := client.StreamJobLog(t.Context(), pb.StreamJobLogRequest_builder{
		JobId: &jobID,
	}.Build())
	require.NoError(t, err)

	var actual []byte
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		actual = append(actual, resp.GetOutput()...)
	}
	assert.Equal(t, expected, actual)
}
