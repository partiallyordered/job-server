# int-backend-matt-1

This repo implements the job server specified in [design.md](design.md).

### Development

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
