package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"testing"

	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/partiallyordered/int-backend-matt-1/internal"
	"github.com/partiallyordered/int-backend-matt-1/server/jobserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func startServer(t *testing.T) *bufconn.Listener {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	creds, err := internal.LoadCreds(
		"../../server/server.crt",
		"../../server/server.key",
		"../../server/client-ca.crt",
	)
	require.NoError(t, err)
	s := grpc.NewServer(grpc.Creds(creds))

	allowlist, err := internal.GenerateAllowList()
	require.NoError(t, err)

	pb.RegisterJobServiceServer(s, jobserver.Create(allowlist))
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
	clientCert, err := tls.LoadX509KeyPair("../client.crt", "../client.key")
	require.NoError(t, err)
	serverCACertPEM, err := os.ReadFile("../server-ca.crt")
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

func TestStartCmd(t *testing.T) {
	lis := startServer(t)
	client := newClient(t, lis)

	cmd := startCmd(client)
	cmd.SetArgs([]string{"start-test-job", "echo", "hello"})
	assert.NoError(t, cmd.Execute())
}

func TestStatusCmd(t *testing.T) {
	lis := startServer(t)
	client := newClient(t, lis)

	jobID := "status-test-job"
	executable := "yes"
	_, err := client.CreateJob(t.Context(), pb.CreateJobRequest_builder{
		JobId:      &jobID,
		Executable: &executable,
	}.Build())
	require.NoError(t, err)

	cmd := statusCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{jobID})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "JOB_STATE_RUNNING")
}

func TestStopCmd(t *testing.T) {
	lis := startServer(t)
	client := newClient(t, lis)

	jobID := "stop-test-job"
	executable := "yes"
	_, err := client.CreateJob(t.Context(), pb.CreateJobRequest_builder{
		JobId:      &jobID,
		Executable: &executable,
	}.Build())
	require.NoError(t, err)

	cmd := stopCmd(client)
	cmd.SetArgs([]string{jobID})
	assert.NoError(t, cmd.Execute())
}

func TestStreamCmd(t *testing.T) {
	lis := startServer(t)
	client := newClient(t, lis)

	jobID := "stream-test-job"
	executable := "echo"
	_, err := client.CreateJob(t.Context(), pb.CreateJobRequest_builder{
		JobId:      &jobID,
		Executable: &executable,
		Args:       []string{"hello"},
	}.Build())
	require.NoError(t, err)

	cmd := streamCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{jobID})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, "hello\n", out.String())
}
