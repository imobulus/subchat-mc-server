ARG WORKUID=0

FROM golang:1.23.4 AS build
USER $WORKUID:$WORKUID
WORKDIR /build
COPY pkg/ pkg/
WORKDIR /build/pkg/cmd/tgauth
RUN mkdir /tgbot
RUN --mount=type=cache,target=/go/pkg go build -o /tgbot/tgauth .
WORKDIR /tgbot

CMD ["./tgauth"]
