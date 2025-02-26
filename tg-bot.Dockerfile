FROM golang:1.23.4 AS build
WORKDIR /build
COPY pkg/ pkg/
WORKDIR /build/pkg/cmd/tgauth
RUN --mount=type=cache,target=/go/pkg go build -o /tgbot/tgauth /build/pkg/cmd/tgauth

FROM golang:1.23.4
ARG WORKUID=0
USER $WORKUID:$WORKUID
WORKDIR /tgbot
COPY --from=build --chown=$WORKUID:$WORKUID /tgbot/tgauth /tgbot/tgauth

CMD ["./tgauth"]
