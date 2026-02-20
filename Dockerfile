FROM golang:1.26.0-alpine3.23 AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./main.go

FROM public.ecr.aws/docker/library/alpine:3.20
RUN adduser -D -u 10001 appuser
WORKDIR /app
COPY --from=builder /app/server ./server
USER appuser
EXPOSE 8080
ENTRYPOINT ["./server"]
