# Use multi-stage build
FROM golang:1.16-alpine

WORKDIR /webhook

COPY go.mod ./
COPY go.sum ./
COPY *.go ./

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /webhook-relayer

EXPOSE 2110

CMD [ "/webhook-relayer" ]

