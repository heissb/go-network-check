FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY . .

RUN go mod init network-status-api || true
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o network-status-api main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/network-status-api .

EXPOSE 8080

CMD ["./network-status-api"]
