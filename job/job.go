// Package job contains functionality for managing processes on the host system. Functionality
// provided:
// - start/stop of jobs
// - examination of current run state of jobs
// - streaming of standard output streams of jobs
package job

import "context"

// Job represents a subprocess running on the host system. To create a job, use the helper function
// [CreateJob]. Job status and output can be retrieved with [Status] and [Output] respectively. A
// job can be stopped with [Signal], which is so-named because it sends a signal to a job.
type Job struct{}

// OutputStream represents the standard output streams: stdout and stderr.
type OutputStream int

const (
	Stdout OutputStream = iota
	Stderr
)

// JobOutput contains job output and the stream on which it was produced.
type JobOutput struct {
	Data   []byte
	Stream OutputStream
}

// JobStatus represents a job's running/stopped state.
type JobStatus int

const (
	Running JobStatus = iota
	Stopped
)

// CreateJob is a helper function to create a [Job]. Internally, a job is a system process. A
// created job is started immediately. The job (and process) can be stopped either by canceling the
// provided context or by calling the [Signal] method.
func CreateJob(ctx context.Context, executable string, arg ...string) (*Job, error) {
	return nil, nil
}

// Output returns a channel to which is produced all job output since the creation of the job. This
// includes output from both stdout and stderr streams.
func (job *Job) Output() (<-chan JobOutput, error) {
	return nil, nil
}

// Signal sends a signal to the process represented by the job. This is the mechanism by which a
// job is stopped. Timeouts and graceful or forceful termination of processes is not handled in this
// library.
func (job *Job) Signal(signal int) error {
	return nil
}

// Status returns the current running status of the job: [Running] or [Stopped].
func (job *Job) Status() JobStatus {
	return Running
}
