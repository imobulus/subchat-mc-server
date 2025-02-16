FROM alpine:latest
WORKDIR /infrared
ADD --link https://github.com/haveachin/infrared/releases/download/v2.0.0-alpha.r2/infrared_Linux_x86_64.tar.gz infrared.tar.gz
RUN tar -xzf infrared.tar.gz && rm infrared.tar.gz
RUN ./infrared || true

CMD ["./infrared"]
