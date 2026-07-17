FROM golang:1.26 AS build

# Builds from the workspace root dir
WORKDIR /app

# copy workspace and module manifests before sources to leverage Docker cache
COPY go.work go.work.sum ./
COPY directory/go.mod directory/go.sum ./directory/
COPY directory-mock/go.mod directory-mock/go.sum ./directory-mock/
COPY broker/go.mod broker/go.sum ./broker/
COPY httpclient/go.mod httpclient/go.sum ./httpclient/
COPY illmock/go.mod illmock/go.sum ./illmock/
COPY iso18626/go.mod iso18626/go.sum ./iso18626/
COPY marcxml/go.mod marcxml/go.sum ./marcxml/
COPY ncip/go.mod ncip/go.sum ./ncip/
COPY sru/go.mod sru/go.sum ./sru/
COPY testutil/go.mod testutil/go.sum ./testutil/
COPY zoom/go.mod zoom/go.sum ./zoom/

# Set destination for COPY
WORKDIR /app/directory

# sources are generated before docker build
# make sure make generate is run

# see .dockerignore for what is getting copied
COPY directory/ ./

# download go deps, caches GOMODPATH
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
  go mod download

# Build, caches GOCACHE
RUN --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
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

# copy binaries
COPY --from=build /directory .
# copy migrations
COPY --from=build /app/directory/migrations /migrations

ENV HTTP_PORT=8086
EXPOSE ${HTTP_PORT}

# Run
USER directory-user:directory-user
CMD ["/directory"]
