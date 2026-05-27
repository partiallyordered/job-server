// Package job contains functionality for managing processes on the host system. Functionality
// provided:
// - start/stop of jobs
// - examination of current run state of jobs
// - streaming of standard output streams of jobs
package job

import (
	"context"
)

// Job represents a subprocess running on the host system. To create a job, use the helper function
// [CreateJob]. Job status and output can be retrieved with [Status] and [Output] respectively. A
// job can be stopped with [Signal], so-named because it sends a signal to a job.
type Job struct{}

// StatusCode represents a job's running/stopped state.
type StatusCode int

const (
	// Running indicates a job that has been created/started and not yet stopped
	Running StatusCode = iota
	// StoppedNormally indicates a job that has been created/started and has completed normally or been
	// stopped by a user signal. Check [Status.ExitCode] to determine whether the job completed
	// normally or was stopped by the user (exit code -1).
	StoppedNormally
	// ExternallyStopped indicates that the job was stopped by a signal issued external to the job
	// manager, for example the OOMKiller.
	ExternallyStopped
)

// Status provides a status code to indicate whether the job is stopped or running. The exit code
// will not be set when the job has not finished. An exit code of -1 indicates the job was
// terminated forcefully, by [Stop].
type Status struct {
	Code     StatusCode
	ExitCode int
}

// CreateJob is a helper function to create a [Job]. Internally, a job is a system process. A
// created job is started immediately. The job (and process) can be stopped either by canceling the
// provided context or by calling the [Stop] method.
func CreateJob(ctx context.Context, executable string, arg ...string) (*Job, error) {
	return nil, nil
}

// NewOutputReader returns a reader to which is produced all job output since the creation of the
// job. The reader will continue to stream until the job finishes or is stopped.
func (job *Job) NewOutputReader(ctx context.Context) (*cursor, error) {
	return nil, nil
}

// Stop signals the process with SIGKILL to stop it immediately. Timeouts and graceful termination
// are not handled in this library.
func (job *Job) Stop() error {
	return nil
}

// Status returns the current status of the job.
func (job *Job) Status() Status {
	return Status{}
}
