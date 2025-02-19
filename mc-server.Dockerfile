ARG JDK_VERSION=21

FROM alpine:latest AS mods
WORKDIR /mcserver
RUN mkdir mods clientmods
ADD --link https://cdn.modrinth.com/data/P7dR8mSH/versions/3WOjLgFJ/fabric-api-0.116.1%2B1.21.4.jar mods/fabric-api.jar
RUN cp mods/fabric-api.jar clientmods
ADD --link https://cdn.modrinth.com/data/aZj58GfX/versions/cNfqAFbs/easyauth-mc1.21.2-3.0.27.jar mods/easyauth.jar
ADD --link https://cdn.modrinth.com/data/Vebnzrzj/versions/6h9SnsZu/LuckPerms-Fabric-5.4.150.jar mods/luckperms.jar

FROM golang:1.23.4 AS runscript
WORKDIR /build
COPY pkg/ pkg/
WORKDIR /build/pkg/cmd/runserver
RUN --mount=type=cache,target=/go/pkg go build -o runserver .

FROM openjdk:$JDK_VERSION-jdk-slim AS build-mc-server
ARG \
  MC_VERSION=1.21.4 \
  FABRIC_LOADER_VERSION=0.16.10 \
  FABRIC_INSTALLER_VERSION=1.0.1
WORKDIR /mcserver
ADD --link https://meta.fabricmc.net/v2/versions/loader/$MC_VERSION/$FABRIC_LOADER_VERSION/$FABRIC_INSTALLER_VERSION/server/jar fabric.jar
# initial run to download the server
RUN echo eula=true > eula.txt
RUN java -Xmx2G -jar fabric.jar --nogui --initSettings
COPY --from=mods /mcserver/ ./
COPY --from=runscript /build/pkg/cmd/runserver/runserver runserver
COPY minecraft-server/easyauth.json mods/EasyAuth/config.json
VOLUME /mcserver/player-lists
RUN CONFFILES="banned-ips.json banned-players.json ops.json usercache.json whitelist.json"; \
  for file in $CONFFILES; do \
    ln -s /mcserver/player-lists/$file $file; \
  done

CMD ["./runserver", "--config", "server-config.yaml"]
