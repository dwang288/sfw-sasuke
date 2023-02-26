FROM golang:1.19-alpine3.17 AS build

WORKDIR /app/

COPY . .

RUN go mod download
RUN cd cmd && go build -o ../sfw-sasuke

FROM alpine:latest

WORKDIR /app/

ENV ASSETS_DIR=static
ENV CMD_METADATA_PATH=env/files-metadata.json
COPY --from=build /app/sfw-sasuke .
COPY --from=build /app/env /env
COPY --from=build /app/static /static

CMD ["./sfw-sasuke"]
