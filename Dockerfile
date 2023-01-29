FROM golang:1.19-alpine3.17 AS build

WORKDIR /app/

COPY . .

RUN go mod download
RUN go build

FROM alpine:latest

WORKDIR /app/

COPY --from=build sfw-sasuke .
COPY --from=build /env .
COPY --from=build /static .

CMD ["./sfw-sasuke"]
