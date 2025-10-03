FROM golang:1.25 AS build

# Builds from the workspace root dir
WORKDIR /app
COPY go.work go.work.sum ./

# copy only go.mods so Go doesn't complain on missing workspace modules
COPY illmock/go.mod ./illmock/go.mod

# copy common sources
COPY common/ common
COPY httpclient/ httpclient
COPY marcxml/ marcxml
COPY sru/ sru
COPY iso18626/ iso18626
COPY ncip/ ncip

# Set destination for COPY
WORKDIR /app/broker

# sources are generated before docker build
# make sure make generate is run

# see .dockerignore for what is getting copied
COPY broker/ ./

# download go deps, caches GOMODPATH
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
  go mod download

# Build, caches GOCACHE
RUN --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
  CGO_ENABLED=0 \
  GOOS=linux \
  go build -o /broker ./cmd/broker

RUN --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
  CGO_ENABLED=0 \
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

# create small runtime image
FROM scratch

# need to copy SSL certs and runtime use
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group

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
