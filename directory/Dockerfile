FROM golang:1.23.4 AS build

# Builds from the workspace root dir
WORKDIR /app

# copy only go.mods so Go doesn't complain on missing workspace modules
COPY ./go.mod ./go.mod

# Set destination for COPY
WORKDIR /app/directory

# sources are generated before docker build
# make sure make generate is run

# see .dockerignore for what is getting copied
COPY . ./

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
