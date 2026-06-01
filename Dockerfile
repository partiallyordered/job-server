FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /usr/local/bin/server ./server

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends iputils-ping
# TODO: mount secrets instead of embedding them in a container image
COPY ./server/server.key ./server/server.crt ./server/client-ca.crt /certs
COPY --from=build /usr/local/bin/server /usr/local/bin/server
WORKDIR /certs
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
