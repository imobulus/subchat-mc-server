ARG JDK_VERSION=21

FROM golang:1.23.4 AS modsscript
WORKDIR /build
COPY pkg/ pkg/
WORKDIR /build/pkg/cmd/modssetup
RUN --mount=type=cache,target=/go/pkg go build -o /modssetup .

FROM alpine:latest AS mods
RUN apk --no-cache add zip
COPY --from=modsscript /lib/x86_64-linux-gnu/libc.so.6 /lib/x86_64-linux-gnu/libc.so.6
COPY --from=modsscript /lib64/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2
WORKDIR /mcserver
COPY --from=modsscript /modssetup modssetup
RUN mkdir -p mods clientmods/mods
COPY server-configs/mods.json .
RUN --mount=type=cache,target=cache ./modssetup

FROM golang:1.23.4 AS runscript
WORKDIR /build
RUN --mount=type=cache,target=/go/pkg GOBIN=/build go install github.com/liderman/leveldb-cli@latest
COPY pkg/ pkg/
WORKDIR /build/pkg/cmd/runserver
RUN --mount=type=cache,target=/go/pkg go build -o runserver .

FROM openjdk:$JDK_VERSION-jdk-slim AS build-mc-server
ARG \
  MC_VERSION=1.21.4 \
  FABRIC_LOADER_VERSION=0.16.10 \
  FABRIC_INSTALLER_VERSION=1.0.1
ARG WORKUID=0
USER $WORKUID:$WORKUID
WORKDIR /mcserver
ADD --chown=$WORKUID:$WORKUID --link https://meta.fabricmc.net/v2/versions/loader/$MC_VERSION/$FABRIC_LOADER_VERSION/$FABRIC_INSTALLER_VERSION/server/jar fabric.jar
# initial run to download the server
RUN echo eula=true > eula.txt
RUN java -Xmx2G -jar fabric.jar --nogui --initSettings
COPY --chown=$WORKUID:$WORKUID --from=mods /mcserver/ ./
COPY --chown=$WORKUID:$WORKUID --from=runscript /build/pkg/cmd/runserver/runserver runserver
COPY --chown=$WORKUID:$WORKUID --from=runscript /build/leveldb-cli leveldb-cli
VOLUME /mcserver/player-lists
RUN CONFFILES="banned-ips.json banned-players.json ops.json usercache.json whitelist.json"; \
  for file in $CONFFILES; do \
    ln -s /mcserver/player-lists/$file $file; \
  done

CMD ["./runserver", "--config", "server-config.yaml"]
