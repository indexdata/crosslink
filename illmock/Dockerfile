FROM golang:1.24 AS build

# create go cache dirs in case they are mounted
RUN mkdir -p /root/.cache/go-build /go/pkg/mod

# Builds from the workspace root dir
WORKDIR /app
COPY go.work go.work.sum ./

# copy only go.mods so Go doesn't complain on missing workspace modules
COPY broker/go.mod ./broker/go.mod

# copy common sources
COPY common/ ./common
COPY iso18626/ ./iso18626
COPY marcxml/ ./marcxml
COPY sru/ ./sru

# Set destination for COPY
WORKDIR /app/illmock

# sources are generated before docker build
# make sure make generate is run

# see .dockerignore for what is getting copied
COPY illmock/ ./

# download go deps
RUN go mod download

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /illmock ./cmd/illmock

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
