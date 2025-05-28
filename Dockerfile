FROM golang:1.24

# Builds from the workspace root dir
WORKDIR /app
COPY main.go go.mod go.sum ./
COPY app/ app
COPY test/ test

# download go deps, caches GOMODPATH
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
  go mod download

# Build, caches GOCACHE
RUN --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
  CGO_ENABLED=0 \
  GOOS=linux \
  go build -o mod-dms main.go

# create runtime user
RUN adduser \
  --disabled-password \
  --gecos "" \
  --home "/nonexistent" \
  --shell "/sbin/nologin" \
  --no-create-home \
  --uid 65532 \
  mod-dms-user

ENV HTTP_PORT=8086
EXPOSE ${HTTP_PORT}

# Run
USER mod-dms-user:mod-dms-user
CMD ["/app/mod-dms"]
