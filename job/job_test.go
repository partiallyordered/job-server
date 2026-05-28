package job

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TODO: test that when a job is "stopped externally" its exit code reflects that

func TestEchoHello(t *testing.T) {
	greeting := "hello"
	expected := []byte(greeting)
	job, err := CreateJob("echo", "-n", greeting)
	require.NoError(t, err)
	require.Equal(t, Running, job.Status().Code)
	r := job.NewOutputReader(t.Context())
	result, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, StoppedNormally, job.Status().Code)
	assert.Equal(t, 0, job.Status().LinuxExitCode)
	assert.Equal(t, expected, result)
}

func TestNonZeroExitCode(t *testing.T) {
	code := 42
	job, err := CreateJob("bash", "-c", fmt.Sprintf("exit %d", code))
	require.NoError(t, err)
	require.Equal(t, Running, job.Status().Code)
	_, err = io.ReadAll(job.NewOutputReader(t.Context())) // blocks until after job completion
	require.NoError(t, err)
	assert.Equal(t, StoppedNormally, job.Status().Code)
	assert.Equal(t, code, job.Status().LinuxExitCode)
}

func TestStderrOutput(t *testing.T) {
	greeting := "hello"
	expected := []byte(greeting)
	job, err := CreateJob("bash", "-c", "echo -n "+greeting+" >&2")
	require.NoError(t, err)
	require.Equal(t, Running, job.Status().Code)
	r := job.NewOutputReader(t.Context())
	result, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, StoppedNormally, job.Status().Code)
	assert.Equal(t, 0, job.Status().LinuxExitCode)
	assert.Equal(t, expected, result)
}

func TestStderrAndStdoutOutput(t *testing.T) {
	greeting := "hello"
	expected := append([]byte(greeting), greeting...)
	job, err := CreateJob("bash", "-c", "echo -n "+greeting+" | tee /dev/stderr")
	require.NoError(t, err)
	reader := job.NewOutputReader(t.Context())
	output, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, StoppedNormally, job.Status().Code)
	assert.Equal(t, 0, job.Status().LinuxExitCode)
	assert.Equal(t, expected, output)
}

func TestForcedTermination(t *testing.T) {
	job, err := CreateJob("sleep", "infinity")
	require.NoError(t, err)
	require.NoError(t, job.Stop())
	_, err = io.ReadAll(job.NewOutputReader(t.Context())) // blocks until after job completion
	require.NoError(t, err)
	assert.Equal(t, StoppedNormally, job.Status().Code)
	assert.Equal(t, -1, job.Status().LinuxExitCode)
}
