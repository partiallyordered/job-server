// Program server implements a gRPC job server. It accepts client connections secured with mTLS,
// authenticating clients by the email SAN in their certificate and authorizing them against a
// static allowlist of permitted executables. It exposes RPCs to create jobs, stream their output,
// stop them, and query their status.
package main

import (
	"fmt"
	"log"
	"net"

	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/partiallyordered/int-backend-matt-1/internal"
	"github.com/partiallyordered/int-backend-matt-1/server/jobserver"
	"google.golang.org/grpc"
)

// run is essentially "main that runs defers"
func run() error {
	creds, err := internal.LoadCreds("server.crt", "server.key", "client-ca.crt")
	if err != nil {
		return fmt.Errorf("loading TLS credentials: %w", err)
	}
	// TODO: expose the listener parameters to config
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		return err
	}
	s := grpc.NewServer(grpc.Creds(creds))
	defer s.Stop()
	pb.RegisterJobServiceServer(s, &jobserver.JobServer{})
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
