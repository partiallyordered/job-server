package main

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/partiallyordered/int-backend-matt-1/client/cmd"
	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TODO: test the client validates the server cert
const serverAddr = "localhost:8080"

func loadCreds() (credentials.TransportCredentials, error) {
	clientCert, err := tls.LoadX509KeyPair("client.crt", "client.key")
	if err != nil {
		return nil, fmt.Errorf("loading client cert/key (expected in working directory): %w", err)
	}
	if _, ok := clientCert.PrivateKey.(ed25519.PrivateKey); !ok {
		return nil, errors.New("client private key must be Ed25519")
	}

	serverCaPem, err := os.ReadFile("server-ca.crt")
	if err != nil {
		return nil, fmt.Errorf("reading server CA cert (expected in working directory): %w", err)
	}
	block, _ := pem.Decode(serverCaPem)
	if block == nil {
		return nil, errors.New("failed to decode server CA cert PEM")
	}
	serverCaCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing server CA cert: %w", err)
	}
	if _, ok := serverCaCert.PublicKey.(ed25519.PublicKey); !ok {
		return nil, errors.New("server CA cert must use an Ed25519 key")
	}

	pool := x509.NewCertPool()
	pool.AddCert(serverCaCert)

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
		// TODO: VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error,
	}), nil
}

func newClient() (pb.JobServiceClient, *grpc.ClientConn, error) {
	creds, err := loadCreds()
	if err != nil {
		return nil, nil, err
	}
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to %s: %w", serverAddr, err)
	}
	return pb.NewJobServiceClient(conn), conn, nil
}

func main() {
	client, conn, err := newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	cmd.Execute(client)
}
