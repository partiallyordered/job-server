package cmd

import (
	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/spf13/cobra"
)

func startCmd(client pb.JobServiceClient) *cobra.Command {
	return &cobra.Command{
		Use:   "start <job-id> -- <executable> [args...]",
		Short: "Start a job",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			executable := args[1]
			execArgs := args[2:]
			_, err := client.CreateJob(cmd.Context(), pb.CreateJobRequest_builder{
				JobId:      &jobID,
				Executable: &executable,
				Args:       execArgs,
			}.Build())
			return err
		},
	}
}
