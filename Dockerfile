FROM golang:1.24-alpine AS builder

WORKDIR /app


# build
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o main

# runtime

FROM scratch
COPY --from=builder /app/main /

EXPOSE 8086

ENTRYPOINT ["/main"]
