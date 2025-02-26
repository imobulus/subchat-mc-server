FROM golang:1.23.4 AS build
ARG WORKUID=0
USER $WORKUID:$WORKUID
WORKDIR /build
COPY pkg/ pkg/
WORKDIR /tgbot
WORKDIR /build/pkg/cmd/tgauth
RUN --mount=type=cache,target=/go/pkg,uid=$WORKUID,gid=$WORKUID go build -o /tgbot/tgauth /build/pkg/cmd/tgauth
WORKDIR /tgbot

CMD ["./tgauth"]
