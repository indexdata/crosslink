FROM golang:1.25 AS build

# Builds from the workspace root dir
WORKDIR /app
COPY go.work go.work.sum ./

# copy only go.mods so Go doesn't complain on missing workspace modules
COPY broker/go.mod ./broker/go.mod

# copy common sources
COPY common/ ./common
COPY directory/ directory
COPY httpclient/ ./httpclient
COPY iso18626/ ./iso18626
COPY ncip/ ./ncip
COPY marcxml/ ./marcxml
COPY sru/ ./sru

# copy illmock manifests before sources to leverage docker cache for mod downloads
COPY illmock/go.mod illmock/go.sum ./illmock/

# Set destination for COPY
WORKDIR /app/illmock

# download go deps
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
go mod download
# see .dockerignore for what is getting copied

# sources are generated before docker build
# make sure make generate is run
COPY illmock/ ./

# Build
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=linux \
    go build -o /illmock ./cmd/illmock

# create runtime user
RUN adduser \
  --disabled-password \
  --gecos "" \
  --home "/nonexistent" \
  --shell "/sbin/nologin" \
  --no-create-home \
  --uid 65532 \
  illmock-user

# create small runtime image
FROM scratch

# need to copy SSL certs and runtime use
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group

# copy the binary
COPY --from=build /illmock .

ENV HTTP_PORT=8081
EXPOSE ${HTTP_PORT}

# Run
USER illmock-user:illmock-user
CMD ["/illmock"]
