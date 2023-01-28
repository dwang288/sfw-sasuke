FROM golang:1.19-alpine3.17

WORKDIR /app/

COPY . .

RUN go mod download
RUN go build

CMD ["./sfw-sasuke"]
