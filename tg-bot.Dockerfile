FROM golang:1.23.4 AS build
WORKDIR /build
COPY pkg/ pkg/
WORKDIR /build/pkg/cmd/tgauth
RUN --mount=type=cache,target=/go/pkg go build -o /tgbot/tgauth /build/pkg/cmd/tgauth

FROM alpine:latest
COPY --from=build /lib/x86_64-linux-gnu/libc.so.6 /lib/x86_64-linux-gnu/libc.so.6
COPY --from=build /lib64/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2
ARG WORKUID=0
USER $WORKUID:$WORKUID
WORKDIR /tgbot
COPY --from=build --chown=$WORKUID:$WORKUID /tgbot/tgauth /tgbot/tgauth

CMD ["./tgauth"]
