FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /app/app .

FROM alpine:latest AS final

WORKDIR /app

COPY --from=builder /app/app .

RUN chmod +x /app/app

ENTRYPOINT ["/app/app"]
