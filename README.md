# int-backend-matt-1

This repo implements the job server specified in [design.md](design.md).

### Run

Requirements: quite roughly, go 1.26 on a Linux machine. It's difficult to guarantee support even for that as "Linux" isn't a very well-defined target and the software has only been run with go 1.26 on a single machine.

#### Server

```sh
cd server/
go build -o server
./server
```

##### With Podman/Docker

You can alternatively run the server as follows (if you have docker instead of podman, that _should_ work equally well):

```sh
podman build -t int-backend-matt-1-server .
# --cap-add NET_RAW is required for ping; which is a nice demonstration of the program functionality
podman run --cap-add NET_RAW -p 8080:8080 int-backend-matt-1-server
```

#### Client

```sh
cd client/
go build -o job
./job start my-ping -- ping 127.0.0.1
./job status my-ping
./job stream my-ping
./job stop my-ping
```

### Development

#### Test

```sh
go test -v ./...
```

If command tests are failing, it's possibly (hopefully) because the hard-coded allowlist in [jobserver.go](./server/jobserver/jobserver.go) has binaries at a different location to the binaries in your system. Try to update the allowlist to match your system binary locations (you can use `which` to determine that, with e.g. `which ping`) and re-run the tests.

#### Regenerate gRPC

```sh
go generate ./...
```

#### Secret Generation

```sh
pushd server
../gen-ca-and-self-signed-cert.bash "server" "DNS:localhost,IP:127.0.0.1"
mv server-ca.crt ../client/
popd
pushd client
../gen-ca-and-self-signed-cert.bash "client" "email:int-backend-matt-1-client@domain.local"
mv client-ca.crt ../server/
popd
```

#### TODO

- share allowlists with clients (statically, as in a file, or dynamically, as in a request)- as implemented, it's trial-and-error
