// Package job contains functionality for managing processes on the host system. Functionality
// provided:
// - start/stop of jobs
// - examination of current run state of jobs
// - streaming of standard output streams of jobs
package job

import (
	"context"
	"io"
)

// Job represents a subprocess running on the host system. To create a job, use the helper function
// [CreateJob]. Job status and output can be retrieved with [Status] and [Output] respectively. A
// job can be stopped with [Signal], so-named because it sends a signal to a job.
type Job struct{}

// OutputStream represents the standard output streams: stdout and stderr.
type OutputStream int

const (
	Stdout OutputStream = iota
	Stderr
)

// JobStatusCode represents a job's running/stopped state.
type JobStatusCode int

const (
	Running JobStatusCode = iota
	Stopped
)

// JobStatus provides a status code to indicate whether the job is stopped or running, and a flag
// to indicate whether the job has written data to stdout and/or stderr. These flags make it
// easier for the consumer to see which buffers might be interesting to read. They especially help
// to surface the case where a job has written a warning or error to stderr before exiting, or
// after exiting normally.
type JobStatus struct {
	Code          JobStatusCode
	StdoutWritten bool
	StderrWritten bool
	LinuxExitCode uint8
}

// CreateJob is a helper function to create a [Job]. Internally, a job is a system process. A
// created job is started immediately. The job (and process) can be stopped either by canceling the
// provided context or by calling the [Signal] method.
func CreateJob(ctx context.Context, executable string, arg ...string) (*Job, error) {
	return nil, nil
}

// Output returns a reader to which is produced all job output since the creation of the job. The
// caller specifies which output stream to read.
func (job *Job) Output(output OutputStream) (io.Reader, error) {
	return nil, nil
}

// Signal sends a signal to the process represented by the job. This is the mechanism by which a
// job is stopped. Timeouts and graceful or forceful termination of processes are not handled in
// this library.
func (job *Job) Signal(signal int) error {
	return nil
}

// Status returns the current running status of the job: [Running] or [Stopped].
func (job *Job) Status() JobStatus {
	return JobStatus{}
}
