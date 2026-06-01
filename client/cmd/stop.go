package cmd

import (
	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/spf13/cobra"
)

func stopCmd(client pb.JobServiceClient) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <job-id>",
		Short: "Stop a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			_, err := client.StopJob(cmd.Context(), pb.StopJobRequest_builder{
				JobId: &jobID,
			}.Build())
			return err
		},
	}
}
