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

11. The _user_ (i.e. the caller of the CLI) will be responsible for interleaving stderr and stdout. This assumption has the following implications:
    - The implementation is simplified, with all the advantages of that (testability, reliability, maintainability, implementation time, etc.)
    - stderr and stdout are vanishingly unlikely to be produced on e.g. a terminal or in a log in the same chronological order they were produced by the job. Although to ensure this would considerably increase the implementation complexity, and there's not even a guarantee the job itself deterministically produces output on the two streams. However, this is likely to also mean that correlating events on stdout and warnings or errors on stderr becomes more difficult.
    - The user will more easily be able to distinguish between stderr and stdout streams. This means that if stdout produces a lot of data, and the job writes an error or warning to stderr, this can be more visible.
    - The user will not necessarily see that an error occurred, and is even less likely to see warnings. Some facility to surface these will be provided in the implementation.

# Design Approach

## API

The _design approach_ section begins with the API, by encouraging the reader to review the [gRPC API spec](./job_service/v1/job_manager.proto) contained in this repository. This will provide useful context when reading the rest of this section.

## Worker Library

The worker library will expose an abstract representation of a job. Each job will store its state internally, specifically the running process represented by the job, and all process output. Under the aforementioned assumption of unlimited memory, no effort is made to constrain memory consumption of job output.

Internally, the worker will maintain two output buffers, each with a single writer, to which it will write stderr and stdout produced by the job. Each time a call is made to GetOutput, a new reader will be created for the requested buffer, stdout or stderr. This reader will block when it reaches the head of the buffer. To appropriately coordinate access to the shared data, a sync.RWMutex will be used. To avoid unnecessary resource usage Goroutines will be suspended and awoken using sync.Cond. Compared with naively writing to channels, this approach avoids only producing output as fast as the slowest reader.

## Server

The server will handle authentication and authorization. It will store a collection of jobs, which it will manage as requested by the client using the gRPC API specified in this repository.

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

Stop a job with `job stop <id>`. This will send a SIGTERM to the process to allow it to exit gracefully.

```sh
job stop enthusiastic-agreement
# Or immediately. Other signals are available, meaning the user can e.g. pause and resume jobs.
job stop --signal=SIGKILL enthusiastic-agreement
# Gracefully, but with a timeout, after which the process will be sent a SIGKILL.
job stop --timeout=5s enthusiastic-agreement
```

Stream the output of a job with `job stream [--output] <job-id>`:

```bash
# The default output stream is stdout.
job stream enthusiastic-agreement
# But we can stream stderr if we like. This will be produced on the stdout of `job`.
job stream --output=stderr enthusiastic-agreement
# Multiplex stdout and stderr using shell functionality, e.g. a simple (slightly awkward) example in bash:
job stream enthusiastic-agreement & job stream --output=stderr enthusiastic-agreement
```

It's important to note that stdout and stderr may not be reproduced in the same order as by the job, though they will nonetheless be produced on the CLI stdout and stderr.

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
