//go:generate protoc -I proto --go_out=gen --go_opt=paths=source_relative --go-grpc_out=gen --go-grpc_opt=paths=source_relative proto/job_service/v1/job_manager.proto

// Package generate is used only to generate gRPC code
package generate
