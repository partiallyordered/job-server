// Package jobserver implements a gRPC job server. It accepts client connections secured with mTLS,
// authenticating clients by the email SAN in their certificate and authorizing them against a
// static allowlist of permitted executables. It exposes RPCs to create jobs, stream their output,
// stop them, and query their status.
package jobserver

import (
	"context"
	"errors"
	"io"
	"log"
	"math"
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
		"/usr/bin/ping",
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
// case. If the identity is permitted to execute the executable, the absolute path of the
// executable will be returned. This means the caller can avoid resolving the path a second time
// with a potentially different result (e.g. if PATH has changed between the two invocations).
func resolveAllowed(identity string, executable string) (string, error) {
	allowed, ok := clientAllowlist[identity]
	if !ok {
		return "", status.Errorf(codes.PermissionDenied, "client %q not present in system", identity)
	}
	resolvedPath, err := exec.LookPath(executable)
	if err != nil {
		return "", status.Errorf(codes.InvalidArgument, "executable %q not found: %v", executable, err)
	}
	if slices.Contains(allowed, resolvedPath) {
		return resolvedPath, nil
	}
	return "", status.Errorf(
		codes.PermissionDenied,
		"executable %q not permitted for client %q",
		executable,
		identity,
	)
}

// authorizeExecutable authorizes or refuses a request to start a job with a given executable based
// on the subject alternative name provided by the peer. The peer should be found in the context
// supplied to authorizeExecutable. If the peer is permitted to execute the executable, the absolute
// path of the executable will be returned. This means the caller can avoid resolving the path a
// second time with a potentially different result (e.g. if PATH has changed between the two
// invocations).
func authorizeExecutable(ctx context.Context, executable string) (string, error) {
	identity, err := resolveSAN(ctx)
	if err != nil {
		return "", err
	}
	return resolveAllowed(identity, executable)
}

// JobServer implements the Job service JobServer defined in the protocol buffer spec in this repository.
// It embeds a map to manage jobs.
type JobServer struct {
	pb.UnimplementedJobServiceServer
	jobs sync.Map
}

// loadJob looks up a [job.Job] corresponding to the supplied job ID. If no corresponding Job is
// found this function will return (nil, nil). If a job is found, this function will return
// (*job, nil).
func loadJob(m *sync.Map, k string) (*job.Job, error) {
	found, ok := m.Load(k)
	// The job could be nil if the name has been reserved but the job not yet created. In this case,
	// readers should consider that there is no job.
	if !ok || found == nil {
		return nil, nil
	}
	j, ok := found.(*job.Job)
	if !ok {
		return nil, errors.New("loading job: job found not of correct type")
	}
	return j, nil
}

// CreateJob creates a job and stores it on the server. Authorization is handled by checking the
// peer subject alternative name against an allowlist of peer/binary pairs. If the peer/binary is
// present, the executable will be allowed to run.
func (s *JobServer) CreateJob(
	ctx context.Context,
	req *pb.CreateJobRequest,
) (*pb.CreateJobResponse, error) {
	jobID := req.GetJobId()
	if jobID == "" {
		// We could support the empty string as a job ID, but we do not, because this could be extremely
		// confusing in some contexts.
		return nil, status.Error(codes.InvalidArgument, "job ID must have non-zero length")
	}
	execPathAbs, err := authorizeExecutable(ctx, req.GetExecutable())
	if err != nil {
		return nil, err
	}

	// We reserve the job ID here then start the job to avoid a race when storing the job later.
	_, alreadyPresent := s.jobs.LoadOrStore(jobID, nil)
	if alreadyPresent {
		return nil, status.Error(codes.AlreadyExists, "job ID already exists")
	}
	job, err := job.Create(execPathAbs, req.GetArgs()...)
	if err != nil {
		// Release the reserved job ID in case of failure to create the job
		s.jobs.Delete(jobID)
		return nil, status.Errorf(codes.Internal, "failed to create job: %v", err)
	}
	s.jobs.Store(jobID, job)

	return &pb.CreateJobResponse{}, nil
}

// StreamJobLog streams a job log until completion, or until the client disconnects.
func (s *JobServer) StreamJobLog(
	req *pb.StreamJobLogRequest,
	srv grpc.ServerStreamingServer[pb.StreamJobLogResponse],
) error {
	j, err := loadJob(&s.jobs, req.GetJobId())
	if err != nil {
		return status.Errorf(codes.Internal, "error retrieving job: %v", err)
	}
	if j == nil {
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
			if errors.Is(err, io.EOF) || errors.Is(err, job.ErrCursorClosed) {
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
				return status.Errorf(codes.Internal, "grpc stream terminated unexpectedly: %v", err)
			}
		}
	}
}

// StopJob stops a job.
func (s *JobServer) StopJob(
	ctx context.Context,
	req *pb.StopJobRequest,
) (*pb.StopJobResponse, error) {
	j, err := loadJob(&s.jobs, req.GetJobId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error retrieving job: %v", err)
	}
	if j == nil {
		return nil, status.Error(codes.NotFound, "job not found")
	}
	if err := j.Stop(); err != nil && !errors.Is(err, job.ErrAttemptToStopStoppedJob) {
		return nil, status.Errorf(codes.Internal, "stopping job: %v", err)
	}

	return &pb.StopJobResponse{}, nil
}

// GetJobStatus returns the status of a job.
func (s *JobServer) GetJobStatus(
	_ context.Context,
	req *pb.GetJobStatusRequest,
) (*pb.GetJobStatusResponse, error) {
	j, err := loadJob(&s.jobs, req.GetJobId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error retrieving job: %v", err)
	}
	if j == nil {
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
