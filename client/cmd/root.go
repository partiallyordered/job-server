// Package cmd implements the job CLI commands.
package cmd

import (
	"os"

	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/spf13/cobra"
)

func Execute(client pb.JobServiceClient) {
	// rootCmd represents the base command when called without any subcommands
	rootCmd := &cobra.Command{
		Use:   "job",
		Short: "job connects to a job server to manage jobs on the server machine",
		Long: `Examples:

Start a ping job:
job start ping -- ping 127.0.0.1
The job-name is client-provided and must not have already been used.
Authorization is handled by allowlist on the server.

View the job status:
job status ping

Stream the job log:
job stream ping
Logs will be streamed from the beginning of output.

Stop the job:
job stop ping
A stop request will send a SIGKILL to the process represented by the job.`,
	}

	rootCmd.AddCommand(startCmd(client), statusCmd(client), stopCmd(client), streamCmd(client))
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
