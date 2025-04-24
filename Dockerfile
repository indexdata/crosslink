FROM golang:1.24 AS build

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

# create small runtime image
FROM scratch

# need to copy SSL certs and runtime use
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group

# copy the binary
COPY --from=build /app/mod-dms /

ENV PORT=8086
EXPOSE ${PORT}

# Run
USER mod-dms-user:mod-dms-user
CMD ["/mod-dms"]
