FROM golang:1.26 AS build

RUN apt-get update && apt-get install -y libyaz-dev pkg-config

WORKDIR /app

# Copy manifests first so source changes do not invalidate dependency caches.
COPY broker/go.mod broker/go.sum ./broker/
COPY directory/go.mod directory/go.sum ./directory/
COPY illmock/go.mod illmock/go.sum ./illmock/
COPY directory-mock/go.mod directory-mock/go.sum ./directory-mock/
COPY httpclient/go.mod httpclient/go.sum ./httpclient/
COPY iso18626/go.mod iso18626/go.sum ./iso18626/
COPY marcxml/go.mod marcxml/go.sum ./marcxml/
COPY ncip/go.mod ncip/go.sum ./ncip/
COPY sru/go.mod sru/go.sum ./sru/
COPY testutil/go.mod testutil/go.sum ./testutil/
COPY zoom/go.mod zoom/go.sum ./zoom/

RUN go work init \
    ./broker \
    ./directory \
    ./illmock \
    ./directory-mock \
    ./httpclient \
    ./iso18626 \
    ./marcxml \
    ./ncip \
    ./sru \
    ./testutil \
    ./zoom

RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    cd /app/directory && \
    GOWORK=off go mod download

RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    cd /app/broker && \
    go mod download

# Copy dependent module sources after dependencies have been downloaded.
COPY directory/ ./directory
COPY httpclient/ ./httpclient
COPY marcxml/ ./marcxml
COPY sru/ ./sru
COPY iso18626/ ./iso18626
COPY ncip/ ./ncip
COPY zoom/ ./zoom
COPY testutil/ ./testutil

# Directory generated sources are not stored in the repository.
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    cd /app/directory && \
    GOWORK=off go generate .

WORKDIR /app/broker

# Copy Broker sources after dependencies have been downloaded.
COPY broker/ ./

# Build, reuse both module and build caches
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
    CGO_ENABLED=1 \
    GOOS=linux \
    go build -o /broker ./cmd/broker

RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
    CGO_ENABLED=1 \
    GOOS=linux \
    go build -o /archive ./cmd/archive

# create runtime user
RUN adduser \
  --disabled-password \
  --gecos "" \
  --home "/nonexistent" \
  --shell "/sbin/nologin" \
  --no-create-home \
  --uid 65532 \
  broker-user

# Save list of shared lib deps
RUN ldd /broker | tr -s '[:blank:]' '\n' | grep '^/' | \
    xargs -I % sh -c 'mkdir -p $(dirname deps%); cp % deps%;'

# create small runtime image
FROM scratch

# need to copy SSL certs and runtime use
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group

# copy the shared libs
COPY --from=build /app/broker/deps /

# copy binaries
COPY --from=build /broker .
COPY --from=build /archive .
# copy migrations
COPY --from=build /app/broker/migrations /migrations

ENV HTTP_PORT=8081
EXPOSE ${HTTP_PORT}

# Run
USER broker-user:broker-user
CMD ["/broker"]
