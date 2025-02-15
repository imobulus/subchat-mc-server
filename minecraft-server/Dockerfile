ARG JDK_VERSION=21

FROM alpine:latest AS mods
WORKDIR /mcserver
RUN mkdir mods clientmods
ADD --link https://cdn.modrinth.com/data/P7dR8mSH/versions/3WOjLgFJ/fabric-api-0.116.1%2B1.21.4.jar mods/fabric-api.jar
RUN cp mods/fabric-api.jar clientmods

FROM openjdk:$JDK_VERSION-jdk-slim AS build-mc-server
ARG \
  MC_VERSION=1.21.4 \
  FABRIC_LOADER_VERSION=0.16.10 \
  FABRIC_INSTALLER_VERSION=1.0.1
WORKDIR /mcserver
ADD --link https://meta.fabricmc.net/v2/versions/loader/$MC_VERSION/$FABRIC_LOADER_VERSION/$FABRIC_INSTALLER_VERSION/server/jar fabric.jar
COPY --from=mods /mcserver/ ./
RUN java -Xmx2G -jar fabric.jar nogui
COPY server-conf/server.properties .
RUN echo eula=true > eula.txt
VOLUME world

CMD ["java", "-Xmx8G", "-jar", "fabric.jar"]
