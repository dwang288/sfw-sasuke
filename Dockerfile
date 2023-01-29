FROM golang:1.19-alpine3.17 AS build

WORKDIR /app/

COPY . .

RUN go mod download
RUN go build

FROM alpine:latest

WORKDIR /app/

COPY --from=build /app/sfw-sasuke .
COPY --from=build /app/env /env
COPY --from=build /app/static /static

CMD ["./sfw-sasuke"]
