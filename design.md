# Summary

Implement a prototype job worker service that provides an API to run arbitrary Linux processes. These processes can be any executable program that is available on the machine running the service.

# Scope

## Requirements

The implementation should be split into three conceptual parts: a worker library, a server which consumes the worker library and exposes its functionality as a gRPC API, and a CLI client which consumes the gRPC API.

### Library Requirements

1. Library should be able to stream the output of a job.
2. Discovering new output should be efficient, avoid busy-waiting or polling.
3. Output should be from start of process execution.
4. Multiple concurrent clients should be supported.
5. Do not make any assumptions about the process's output - it may be text or raw binary data.

### API Requirements

1. gRPC API to start/stop/get status/stream output of a job.
2. Use mTLS authentication and verify client certificate. Set up strong set of cipher suites for TLS and good crypto setup for certificates. Do not use any other authentication protocols on top of mTLS.
3. Use a simple authorization scheme.

### CLI Client Requirements

The CLI should be able to connect to the worker service and start, stop, get status, and stream the output of a job.

## Assumptions

Assumptions are made primarily to constrain scope and reduce the duration and complexity of implementation.

1. The server application will be managed manually, there will be no orchestration, no service manager, etc. If it fails, it's restarted manually by an operator. The operator will update the service manually.

2. Job logs will be stored in memory.

3. Memory is unlimited.

4. Job logs will be stored from the beginning of job output.

5. There will be no management of completed jobs, all job logs will rest in the server application memory until exit.

6. Users (not clients) cooperate with each other. We don't really care how. That means sharing server resources (CPU, memory, disk) fairly is out of scope.

7. The server application will inherit the permissions of the user as which it is run. It's therefore up to the operator to use system facilities (file permissions, setuid/setgid, SELinux, cgroups, niceness etc.) to permit/constrain the server application and correspondingly (and especially) the jobs it is able to execute.

8. It's up to the end user to query the job status and handle retry of failed jobs.

9. Jobs will not accept data via stdin.

10. Derived from requirement _output should be from the start of process execution_, clients will stream the log from the beginning at each invocation.

# Design Approach

## API

The _design approach_ section begins with the API, by encouraging the reader to review the [gRPC API spec](./job_service/v1/job_manager.proto) contained in this repository. This will provide useful context when reading the rest of this section.

## Worker Library

The worker library will expose an abstract representation of a job. Each job will store its state internally, specifically the running process represented by the job, and all process output. Under the aforementioned assumption of unlimited memory, no effort is made to constrain memory consumption of job output.

### Handling Process Output

The worker will have an implementation of a buffer that supports multiple readers and multiple writers. As a consequence of using [sync.RWMutex](https://pkg.go.dev/sync#RWMutex) internally, buffer will at any one time support either _a single writer_ **or** _multiple concurrent readers_.

Readers will block when they have reached the write head, i.e. there is no more data to read, and the buffer is not closed. They will wait efficiently by waiting on a signal channel which will be closed when data is written to the buffer, indicating they should wake up and attempt to acquire the read lock.

When the buffer is written, the writer will acquire the write lock, excluding all other threads. The payload will be written and a signal channel will be closed to indicate that readers should wake up for new data. The signal channel will be replaced so the readers can "resubscribe" and the write lock will be released.

When the buffer is closed, an internal flag will be set. When readers reach the end of the buffer and the buffer is closed, EOF will be returned.

See the skeleton of `broadcastBuffer` at [./job/broadcast_buffer.go](./job/broadcast_buffer.go).

### Managing Processes

Processes will be created using [exec.CommandContext](https://pkg.go.dev/os/exec#CommandContext). A single `broadcastBuffer` will be assigned to both `cmd.Stderr` and `cmd.Stdout`, interleaving the process output streams to the buffer. When a job is created, the process will be created and started. A goroutine will be spawned and block on `cmd.Wait`. When the process exits normally or is terminated, this goroutine will resume and close the `broadcastBuffer`. Any readers will continue to read until the end of the buffer, then receive io.EOF, allowing them to release resources. Concurrent calls to `Job.Stop` and `Job.Status`, and changes to their data `job.status` and `job.exitCode` will be protected by mutex.

The planned library API can be see in the skeleton [./job/job.go](./job/job.go).

## Server

The server will handle authentication and authorization. It will store a `map[string]Job` in order to map requests to jobs. It will manage this collection as requested by the client using the gRPC API specified in this repository. When a request to stream is received, the server will request a new output reader from the job, to which it will supply a context to signal client disconnection, allowing resources to be released when clients disappear.

Authorization will be managed by:

1. Identifying the client using the SAN on its TLS certificate
2. Referring to an allowlist of binaries the client is permitted to execute
3. Rejecting requests to execute any binary that is not present in the client's allowlist

The allowlist will be hard-coded to demonstrate functionality and save time on implementation.

## CLI

The CLI will have a subcommand-style API for readability and ergonomics. For simplicity of implementation, it will expect to find its private key and client certificate in its working directory, with hardcoded filenames. This information will be available in the help text (`job help`) and in error messages.

Create a job with `job start <id> <executable> <...args>`:

```sh
job start enthusiastic-agreement yes "yes!"
```

Get the job status

```sh
job status enthusiastic-agreement
```

Stop a job with `job stop <id>`. This will send a SIGKILL to the process.

```sh
job stop enthusiastic-agreement
```

Stream the output of a job with `job stream <job-id>`:

```bash
# The default output stream is stdout.
job stream enthusiastic-agreement
```

# Security Considerations

## Authentication and Transport Security

The client and server will authenticate one another using mTLS, per requirements. As we control both the client and the server, usage of TLS1.3 will be enforced. This removes our choice of ciphers and restricts us to a secure set, addressing transport security. Both the client and server will reject all certificates (their own and each other's) that do not use ed25519 keys.

## Client

It's possible a server could return malicious output that the client could use inadvertently, for example if the server (or a sneaky client) knew the client was streaming output to a shell as a script. No effort will be made to prevent malicious output produced by the server.

## Server

### Host System Integrity

The server operator will be expected to run the server application with the appropriate system facilities to permit or constrain access to system resources. These facilities could be, for example, file permissions, setuid/setgid, SELinux, cgroups and niceness.

### Denial of Service

It's possible one client might create a job that consumes system resources to the exclusion of other clients. There is no consideration given to this scenario in the implementation, as specified by the assumption of cooperative users.

### Shell Access

Of special concern might be the ability of the client to execute arbitrary commands by calling a shell and executing a command inside that shell, e.g. `bash -c 'my-nefarious-binary'`. It is the responsibility of the system operator to manage the executable allowlist to prevent this functionality where appropriate.

### Malicious Client-Client Interference

It's possible clients might interfere with each others' jobs, maliciously or accidentally. As per the assumption of cooperative users, clients are considered also to be cooperative, and therefore no effort will be made to prevent client-client interference.
