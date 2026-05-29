// Package job contains functionality for managing processes on the host system. Functionality
// provided:
// - start/stop of jobs
// - examination of current state of jobs
// - streaming of standard output streams of jobs
package job

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// Job represents a subprocess running on the host system. To create a job, use the helper function
// [Create]. Job status can be retrieved with [Status]. [NewOutputReader] returns a reader that
// will stream stdout and stderr from the job until it completes. A job can be stopped with [Stop].
type Job struct {
	cmd           *exec.Cmd
	output        *broadcastBuffer
	err           error
	status        StatusCode
	mu            sync.Mutex
	exitCode      int
	stopRequested bool
}

// StatusCode represents a job's running/stopped state.
type StatusCode int

const (
	// Running indicates a job that has been created/started and not yet stopped
	Running StatusCode = iota
	// StoppedNormally indicates a job that has been created/started and has completed normally or been
	// stopped by a user signal. Check [Status.ExitCode] to determine whether the job completed
	// normally or was stopped by the user (exit code -1).
	//
	// Note that there is a very slim possibility of returning a job status of StoppedNormally when a
	// job has been stopped externally. This can occur when [Stop] has been called, and while we're
	// waiting for the job to stop, the process is stopped by something external to the job manager.
	// In this case, we will not know whether or not the process was stopped by us.
	StoppedNormally
	// ExternallyStopped indicates that the job was stopped by a signal issued external to the job
	// manager, for example the OOMKiller.
	ExternallyStopped
)

// Status provides a status code to indicate whether the job is stopped or running. The exit code
// will not be set when the job has not finished. An exit code of -1 indicates the job was
// terminated forcefully, by [Stop]. The Error field will be set in case of a system error outside
// of the running job, i.e. an I/O error.
type Status struct {
	ManagerError string
	Code         StatusCode
	ProcExitCode int
}

// ErrAttemptToStopStoppedJob occurs when the user calls [Stop] on a stopped job
var ErrAttemptToStopStoppedJob = errors.New("attempt to stop stopped job")

// createAndStart creates and starts a subprocess, and creates and assigns a buffer to the process
// output streams.
func createAndStart(executable string, arg ...string) (*Job, error) {
	output := newBroadcastBuffer()
	cmd := exec.Command(executable, arg...)

	// TODO: spawn the subprocess in a process group and signal the process group in [Stop]. Didn't do
	// this here because this adds a bit of (especially) error-handling complexity in [Stop] if some
	// processes are successfully signaled and others are not.
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	return &Job{
		cmd:    cmd,
		output: output,
		status: Running,
	}, nil
}

// Create is a helper function to create a [Job]. Internally, a job is a system process. A
// created job is started immediately. The job (and process) can be stopped by calling the [Stop]
// method.
func Create(executable string, arg ...string) (*Job, error) {
	result, err := createAndStart(executable, arg...)
	if err != nil {
		return nil, err
	}

	// spawn a goroutine to wait on the subprocess exit and update the job status etc.
	go func() {
		cmdErr := result.cmd.Wait()
		result.mu.Lock()
		defer result.mu.Unlock()
		if cmdErr != nil {
			if _, ok := errors.AsType[*exec.ExitError](cmdErr); !ok {
				// An ExitError is redundant with the exit code, and is not necessarily an error. We therefore
				// do not return ExitErrors.
				result.err = fmt.Errorf("system error: %w", cmdErr)
			}
		}
		result.exitCode = result.cmd.ProcessState.ExitCode()
		result.status = ExternallyStopped
		if result.stopRequested || result.exitCode != -1 {
			result.status = StoppedNormally
		}
		// Calling close on a closed buffer results in a panic. If calling close elsewhere, make sure to
		// guard against this eventuality.
		// Note that we close the buffer after setting the job status, because a caller is likely to wait
		// on buffer read, then upon receiving io.EOF, read the job status.
		result.output.close()
	}()

	return result, nil
}

// NewOutputReader returns a reader to which is produced all job output since the creation of the
// job. The reader will continue to stream until the job finishes or is stopped.
func (job *Job) NewOutputReader(ctx context.Context) io.ReadCloser {
	return job.output.newReader()
}

// Stop signals the process with SIGKILL to stop it immediately. Timeouts and graceful termination
// are not handled in this library.
func (job *Job) Stop() error {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.status != Running {
		return ErrAttemptToStopStoppedJob
	}
	if err := job.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("error killing process: %w", err)
	}
	job.stopRequested = true
	return nil
}

// Status returns the current running status and a small amount of job metadata.
func (job *Job) Status() Status {
	job.mu.Lock()
	defer job.mu.Unlock()

	var errStr string
	if job.err != nil {
		errStr = job.err.Error()
	}
	return Status{
		ProcExitCode: job.exitCode,
		Code:         job.status,
		ManagerError: errStr,
	}
}
