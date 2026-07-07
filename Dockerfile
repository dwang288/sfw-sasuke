FROM golang:1.26-alpine AS build

WORKDIR /app/

COPY . .

RUN go mod download
RUN go build -o sfw-sasuke ./cmd/bot

FROM alpine:latest

WORKDIR /app/

ENV ASSETS_DIR=static
ENV CMD_METADATA_PATH=env/files-metadata.json
COPY --from=build /app/sfw-sasuke .
COPY --from=build /app/env /app/env
COPY --from=build /app/static /app/static

CMD ["./sfw-sasuke"]
