ARG JDK_VERSION=21
FROM golang:1.23.4 AS build
WORKDIR /build
COPY pkg/ pkg/
WORKDIR /build/pkg/cmd/tgauth
RUN --mount=type=cache,target=/go/pkg go build -o tgauth .

FROM alpine:latest
WORKDIR /tgbot
COPY --from=build /build/pkg/cmd/tgauth/tgauth tgauth
COPY minecraft-server/tg-bot.yaml config.yaml
VOLUME /sqlite/auth.db

CMD ["./tgauth"]
