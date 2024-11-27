FROM golang:1.22

# Builds from the workspace root dir
WORKDIR /app
COPY go.work go.work.sum ./
COPY common/go.mod ./common/go.mod

# Set destination for COPY
WORKDIR /app/broker

# sources are generated before docker build
# make sure make generate is run

# Download Go modules
COPY broker/go.mod broker/go.sum ./
RUN go mod download

# Copy sources
COPY broker/cmd ./cmd
COPY broker/iso18626 ./iso18626

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /broker ./cmd/broker

ENV HTTP_PORT=8081

EXPOSE ${HTTP_PORT}

# Run
CMD ["/server"]
