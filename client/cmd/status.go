package cmd

import (
	"fmt"

	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/spf13/cobra"
)

func statusCmd(client pb.JobServiceClient) *cobra.Command {
	return &cobra.Command{
		Use:   "status <job-id>",
		Short: "Get the status of a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			resp, err := client.GetJobStatus(cmd.Context(), pb.GetJobStatusRequest_builder{
				JobId: &jobID,
			}.Build())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "state: %s\n", resp.GetJobState())
			if resp.GetJobState() != pb.JobState_JOB_STATE_RUNNING {
				fmt.Fprintf(out, "exit code: %d\n", resp.GetProcExitCode())
			}
			if m := resp.GetManagerErr(); m != "" {
				fmt.Fprintf(out, "manager error: %s\n", m)
			}
			return nil
		},
	}
}
