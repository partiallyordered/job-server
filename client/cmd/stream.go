package cmd

import (
	"errors"
	"io"

	pb "github.com/partiallyordered/int-backend-matt-1/gen/job_service/v1"
	"github.com/spf13/cobra"
)

func streamCmd(client pb.JobServiceClient) *cobra.Command {
	return &cobra.Command{
		Use:   "stream <job-id>",
		Short: "Stream the output of a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			stream, err := client.StreamJobLog(cmd.Context(), pb.StreamJobLogRequest_builder{
				JobId: &jobID,
			}.Build())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for {
				resp, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					return nil
				}
				if err != nil {
					return err
				}
				if _, err := out.Write(resp.GetOutput()); err != nil {
					return err
				}
			}
		},
	}
}
