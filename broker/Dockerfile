FROM golang:1.23

# Builds from the workspace root dir
WORKDIR /app
COPY go.work go.work.sum ./
COPY common/go.mod ./common/go.mod
COPY illmock/go.mod ./illmock/go.mod

# Set destination for COPY
WORKDIR /app/broker

# sources are generated before docker build
# make sure make generate is run

# Download Go modules
COPY broker/go.mod broker/go.sum ./
RUN go mod download

# Copy sources
COPY broker/adapter ./adapter
COPY broker/app ./app
COPY broker/client ./client
COPY broker/cmd ./cmd
COPY broker/common ./common
COPY broker/events ./events
COPY broker/handler ./handler
COPY broker/ill_db ./ill_db
COPY broker/iso18626 ./iso18626
COPY broker/repo ./repo
COPY broker/service ./service

# Copy DB migrations
COPY broker/migrations ./migrations

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /broker ./cmd/broker

ENV HTTP_PORT=8081

EXPOSE ${HTTP_PORT}

# Run
CMD ["/server"]
