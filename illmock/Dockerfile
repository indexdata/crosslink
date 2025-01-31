FROM golang:1.23

# Builds from the workspace root dir
WORKDIR /app
COPY go.work go.work.sum ./

# copy only go.mods so Go doesn't complain on missing workspace modules
COPY broker/go.mod ./broker/go.mod

# copy common sources
COPY common/ ./common
COPY iso18626/ ./iso18626

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

ENV HTTP_PORT=8081

EXPOSE ${HTTP_PORT}

# Run
CMD ["/illmock"]
