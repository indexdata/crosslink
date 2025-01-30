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

# see .dockerignore for what is getting copied
COPY broker/ ./

# download go deps
RUN go mod download

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /broker ./cmd/broker

ENV HTTP_PORT=8081

EXPOSE ${HTTP_PORT}

# Run
CMD ["/server"]
