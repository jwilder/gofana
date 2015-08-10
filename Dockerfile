FROM debian:wheezy
MAINTAINER Jason Wilder <jason@influxdb.com>

RUN mkdir /app
WORKDIR /app

ENV GOFANA_VERSION v0.0.6
ADD https://github.com/jwilder/gofana/releases/download/$GOFANA_VERSION/gofana-linux-amd64-$GOFANA_VERSION.tar.gz gofana-linux-amd64-$GOFANA_VERSION.tar.gz
RUN gunzip -c /gofana-linux-amd64-$GOFANA_VERSION.tar.gz | tar -C /app -xvf - > /app/gofana

VOLUME /app/dashboards

EXPOSE 8080
EXPOSE 8443

ENTRYPOINT ["/app/gofana"]
