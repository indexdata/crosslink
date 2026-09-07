FROM golang:1.27 AS build

WORKDIR /app

# Copy manifests first so source changes do not invalidate the dependency cache.
COPY directory/go.mod directory/go.sum ./directory/
COPY testutil/go.mod testutil/go.sum ./testutil/

RUN go work init \
    ./directory \
    ./testutil

WORKDIR /app/directory

RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
  go mod download

# Copy sources after dependencies have been downloaded.
COPY testutil/ /app/testutil/
COPY directory/ ./

# Generate API and database code from the tracked specifications.
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
  go generate .

# Build
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
  --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
  CGO_ENABLED=0 \
  GOOS=linux \
  go build -o /directory ./cmd/directory

# create runtime user
RUN adduser \
  --disabled-password \
  --gecos "" \
  --home "/nonexistent" \
  --shell "/sbin/nologin" \
  --no-create-home \
  --uid 65532 \
  directory-user

# create small runtime image
FROM scratch

# need to copy SSL certs and runtime use
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group

# copy the binary
COPY --from=build /directory .
# copy migrations
COPY --from=build /app/directory/migrations /migrations

ENV HTTP_PORT=8086
EXPOSE ${HTTP_PORT}

# Run
USER directory-user:directory-user
CMD ["/directory"]
