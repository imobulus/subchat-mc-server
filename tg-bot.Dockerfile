FROM golang:1.23.4 AS build
WORKDIR /build
COPY pkg/ pkg/
WORKDIR /build/pkg/cmd/tgauth
RUN mkdir /tgbot
RUN --mount=type=cache,target=/go/pkg go build -o /tgbot/tgauth .
WORKDIR /tgbot
COPY minecraft-server/tg-bot.yaml config.yaml

CMD ["./tgauth"]
