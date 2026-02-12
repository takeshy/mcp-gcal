FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o mcp-gcal .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && mkdir -p /data
COPY --from=builder /app/mcp-gcal /usr/local/bin/mcp-gcal
EXPOSE 8080
ENTRYPOINT ["mcp-gcal"]
CMD ["--mode=http", "--addr=:8080"]
