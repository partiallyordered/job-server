// Program server implements a gRPC job server. It accepts client connections secured with mTLS,
// authenticating clients by the email SAN in their certificate and authorizing them against a
// static allowlist of permitted executables. It exposes RPCs to create jobs, stream their output,
// stop them, and query their status.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"slices"
	"sync"

	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/partiallyordered/int-backend-matt-1/job"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const streamBufSize int = 32 * 1024 // 32 KiB

// clientAllowList should contain the client SAN and a list of absolute system paths the client
// shall be permitted to execute.
// TODO: replace with configurable policy- startup or runtime.
var clientAllowlist = map[string][]string{
	"int-backend-matt-1-client@domain.local": {
		"/usr/bin/true",
		"/usr/bin/cat",
		"/usr/bin/yes",
		"/usr/bin/echo",
	},
}

// resolveSAN extracts a gRPC peer from the supplied context, then progressively refines the peer
// information to retrieve the subject alternative name from the TLS cert. Only e-mail SANs are
// supported.
func resolveSAN(ctx context.Context) (string, error) {
	// The following section should always succeed because the server should reject any client that
	// attempts to connect without a TLS cert.
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "no peer info in context")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "peer is not using TLS")
	}
	certs := tlsInfo.State.PeerCertificates
	if len(certs) == 0 {
		return "", status.Error(codes.Unauthenticated, "client certificate not present")
	}

	// TODO: support other types of SAN.
	emails := certs[0].EmailAddresses
	if len(emails) == 0 {
		return "", status.Error(
			codes.PermissionDenied,
			"client certificate has no email SAN; only email SAN is supported",
		)
	}
	return emails[0], nil
}

// resolveAllowed takes an identity and executable name and determines whether the identity is
// allowed to execute the executable. The identity is normally found with resolveSAN. The
// executable can be an absolute or relative path, and will be resolved with [exec.LookPath] in any
// case.
func resolveAllowed(identity string, executable string) error {
	allowed, ok := clientAllowlist[identity]
	if !ok {
		return status.Errorf(codes.PermissionDenied, "client %q not present in system", identity)
	}
	resolvedPath, err := exec.LookPath(executable)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "executable %q not found: %v", executable, err)
	}
	if slices.Contains(allowed, resolvedPath) {
		return nil
	}
	return status.Errorf(
		codes.PermissionDenied,
		"executable %q not permitted for client %q",
		executable,
		identity,
	)
}

// authorizeExecutable authorizes or refuses a request to start a job with a given executable based
// on the subject alternative name provided by the peer. The peer should be found in the context
// supplied to authorizeExecutable.
func authorizeExecutable(ctx context.Context, executable string) error {
	identity, err := resolveSAN(ctx)
	if err != nil {
		return err
	}
	return resolveAllowed(identity, executable)
}

// server implements the Job service server defined in the protocol buffer spec in this repository.
// It embeds a map to manage jobs.
type server struct {
	pb.UnimplementedJobServiceServer
	jobs jobMap
}

// jobMap wraps a sync.Map in order to add some type-safety. The documentation for sync.Map states:
//
// The Map type is optimized for two common use cases: (1) when the entry for a given key is only
// ever written once but read many times, as in caches that only grow, or (2) when multiple
// goroutines read, write, and overwrite entries for disjoint sets of keys. In these two cases, use
// of a Map may significantly reduce lock contention compared to a Go map paired with a separate
// Mutex or RWMutex.
//
// Our use-case is 2-3 writes before any reads commence- after which there should be no further
// writes. sync.Map should give us some advantage here.
type jobMap struct {
	m sync.Map
}

// Load looks up a [job.Job] corresponding to the supplied job ID. If no corresponding Job is found
// this function will return (nil, false). If a job is found, this function will return
// (*job, true).
func (m *jobMap) Load(k string) (*job.Job, bool) {
	j, ok := m.m.Load(k)
	// The job could be nil if the name has been reserved but the job not yet created. In this case,
	// readers should consider that there is no job.
	if !ok || j == nil {
		return nil, false
	}
	return j.(*job.Job), true
}

// Reserve writes a nil value to the map in order to reserve the job ID before starting the job.
func (m *jobMap) Reserve(k string) bool {
	_, alreadyPresent := m.m.LoadOrStore(k, nil)
	return !alreadyPresent
}

// Delete deletes the value for a key.
// If the key is not in the map, Delete does nothing.
// (copied doc for sync.Map.Delete)
func (m *jobMap) Delete(k string) {
	m.m.Delete(k)
}

// Store stores a [job.Job] indexed by its job ID. If the job ID already exists in the map, an error
// will be returned. When creating a job, the job name is reserved by inserting a nil entry then
// later overriding it.
func (m *jobMap) Store(k string, v *job.Job) error {
	if v == nil {
		return errors.New("attempted to store a nil job")
	}
	m.m.Store(k, v)
	return nil
}

// CreateJob creates a job and stores it on the server. Authorization is handled by checking the
// peer subject alternative name against an allowlist of peer/binary pairs. If the peer/binary is
// present, the executable will be allowed to run.
func (s *server) CreateJob(
	ctx context.Context,
	req *pb.CreateJobRequest,
) (*pb.CreateJobResponse, error) {
	jobID := req.GetJobId()
	if jobID == "" {
		// We could support the empty string as a job ID, but we do not, because this could be extremely
		// confusing in some contexts.
		return nil, status.Error(codes.InvalidArgument, "job ID must have non-zero length")
	}
	if err := authorizeExecutable(ctx, req.GetExecutable()); err != nil {
		return nil, err
	}

	// We reserve the job ID here then start the job to avoid a race when storing the job later.
	if !s.jobs.Reserve(jobID) {
		return nil, status.Error(codes.AlreadyExists, "job ID already exists")
	}
	job, err := job.Create(req.GetExecutable(), req.GetArgs()...)
	if err != nil {
		// Release the reserved job ID in case of failure to create the job
		s.jobs.Delete(jobID)
		return nil, status.Errorf(codes.Internal, "failed to create job: %v", err)
	}
	s.jobs.Store(jobID, job)
	return &pb.CreateJobResponse{}, nil
}

// StreamJobLog streams a job log until completion, or until the client disconnects.
func (s *server) StreamJobLog(
	req *pb.StreamJobLogRequest,
	srv grpc.ServerStreamingServer[pb.StreamJobLogResponse],
) error {
	j, ok := s.jobs.Load(req.GetJobId())
	if !ok {
		return status.Error(codes.NotFound, "job not found")
	}
	reader := j.NewOutputReader()
	// Close the reader when the request is closed
	go func() {
		<-srv.Context().Done()
		if err := reader.Close(); err != nil {
			log.Printf("unexpected/unhandled error closing reader: %v", err)
		}
	}()
	p := make([]byte, streamBufSize)
	for {
		n, err := reader.Read(p)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return status.Errorf(codes.Internal, "reading process output: %v", err)
		}
		if n > 0 {
			resp := pb.StreamJobLogResponse_builder{
				Output: p[:n],
			}.Build()
			if err := srv.Send(resp); err != nil {
				if err := reader.Close(); err != nil {
					log.Printf("unexpected/unhandled error closing reader: %v", err)
				}
				log.Printf("grpc stream terminated unexpectedly: %v", err)
				return nil
			}
		}
	}
}

// StopJob stops a job.
func (s *server) StopJob(
	ctx context.Context,
	req *pb.StopJobRequest,
) (*pb.StopJobResponse, error) {
	j, ok := s.jobs.Load(req.GetJobId())
	if !ok {
		return nil, status.Error(codes.NotFound, "job not found")
	}
	if err := j.Stop(); err != nil {
		return nil, status.Errorf(codes.Internal, "stopping job: %v", err)
	}

	return &pb.StopJobResponse{}, nil
}

// GetJobStatus returns the status of a job.
func (s *server) GetJobStatus(
	_ context.Context,
	req *pb.GetJobStatusRequest,
) (*pb.GetJobStatusResponse, error) {
	j, ok := s.jobs.Load(req.GetJobId())
	if !ok {
		return nil, status.Error(codes.NotFound, "job not found")
	}

	st := j.Status()

	var returnJobState pb.JobState
	switch st.Code {
	case job.ExternallyStopped:
		returnJobState = pb.JobState_JOB_STATE_EXTERNALLY_STOPPED
	case job.Running:
		returnJobState = pb.JobState_JOB_STATE_RUNNING
	case job.StoppedNormally:
		returnJobState = pb.JobState_JOB_STATE_STOPPED_NORMALLY
	default:
		return nil, status.Error(codes.Internal, "unrecognized job status")
	}

	if st.ProcExitCode > math.MaxInt32 || st.ProcExitCode < math.MinInt32 {
		// This really should never happen, but it doesn't cost us much to be sure
		return nil, status.Errorf(codes.Internal, "invalid process exit code %d", st.ProcExitCode)
	}
	returnExitCode := int32(st.ProcExitCode)

	return pb.GetJobStatusResponse_builder{
		JobState:     &returnJobState,
		ProcExitCode: &returnExitCode,
		ManagerErr:   &st.ManagerError,
	}.Build(), nil
}

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

// loadCreds loads the server certificate, private key and client CA cert for mTLS. It will return
// an error if
// - it cannot read the files containing these credentials, or
// - it cannot decode them, or
// - any non-Ed25519 keys are used
func loadCreds() (credentials.TransportCredentials, error) {
	// TODO: normally we'd let the user supply the location of their creds as configuration.
	serverCert, err := tls.LoadX509KeyPair("server.crt", "server.key")
	if err != nil {
		return nil, fmt.Errorf("loading server cert/key: %w", err)
	}

	clientCaCertPem, err := os.ReadFile("client-ca.crt")
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

// run is essentially "main that runs defers"
func run() error {
	creds, err := loadCreds()
	if err != nil {
		return fmt.Errorf("loading TLS credentials: %w", err)
	}
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		return err
	}
	s := grpc.NewServer(grpc.Creds(creds))
	defer s.Stop()
	pb.RegisterJobServiceServer(s, &server{})
	if err := s.Serve(lis); err != nil {
		return err
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("error running server: %s", err.Error())
	}
}
