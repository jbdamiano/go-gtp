FROM ubuntu:20.04
LABEL org.opencontainers.image.authors="jean-bernard.damiano@airnity.com"


WORKDIR /opt
ENV TZ=Europe/Paris
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone && apt update && apt upgrade -y && apt install -y git golang-go iproute2 tcpdump net-tools 


ADD gogtp.tar.gz /go-gtp

RUN cd /go-gtp &&  go build -o enb examples/gw-tester/enb/*.go && go build -o mme examples/gw-tester/mme/*.go && go build -o sgw examples/gw-tester/sgw/*.go   && go build -o mme1 examples/gw-tester/mme1/*.go && go build -o sgw1 examples/gw-tester/sgw1/*.go && mkdir /go-gtp/config 
COPY run.sh /go-gtp 
RUN chmod +x /go-gtp/run.sh

ENTRYPOINT ["/go-gtp/run.sh"]

