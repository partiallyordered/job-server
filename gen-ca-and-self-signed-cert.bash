#!/usr/bin/env bash
# requires bash features: process substitution
set -euxo pipefail
EXTENSIONS="basicConstraints=critical,CA:TRUE,pathlen:0"
CA_EXPIRY_DAYS=3650
CLIENT_EXPIRY_DAYS=365
ENTITY="$1"
CA_SUBJ="/CN=int-matt-backend-1-$ENTITY-ca"
CLIENT_SUBJ="/CN=int-matt-backend-1-$ENTITY"

# Define SANs (comma-separated, no spaces)
SAN_CONFIG="$2"

# 1. Create a Certificate Authority private key
openssl req \
    -extensions v3_req \
    -addext "$EXTENSIONS" \
    -nodes -new \
    -newkey ed25519 \
    -out "$ENTITY-ca.csr" \
    -keyout "$ENTITY-ca.key" \
    -subj "$CA_SUBJ"

# 2. Create your CA self-signed certificate:
openssl x509 \
    -signkey "$ENTITY-ca.key" \
    -days "$CA_EXPIRY_DAYS" \
    -req \
    -in "$ENTITY-ca.csr" \
    -out "$ENTITY-ca.crt" \
    -extfile <(echo "$EXTENSIONS")

# 3. Issue a client certificate by first generating the key, then certificate signing request (or use
#    one provided by an external system):
openssl genpkey -algorithm ed25519 -out "$ENTITY.key"
openssl req -new \
    -key "$ENTITY.key" \
    -out "$ENTITY.csr" \
    -subj "$CLIENT_SUBJ" \
    -addext "subjectAltName=$SAN_CONFIG"

# 4. Sign the certificate using private key of your CA. Note that this creates a `ca.srl` file to
#    keep track of the cert serial.
openssl x509 -req \
    -days "$CLIENT_EXPIRY_DAYS" \
    -in "$ENTITY.csr" \
    -CA "$ENTITY-ca.crt" \
    -CAcreateserial \
    -CAkey "$ENTITY-ca.key" \
    -out "$ENTITY.crt" \
    -extfile <(echo "subjectAltName=$SAN_CONFIG")

rm "$ENTITY-ca.csr" "$ENTITY.csr" "$ENTITY-ca.srl"
