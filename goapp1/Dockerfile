FROM golang:latest

WORKDIR /app
COPY go.mod /app/
COPY go.sum /app/
RUN go mod download
COPY *.go /app/
RUN go build -o /app/main

CMD ["/app/main"]