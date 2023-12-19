FROM golang:1.20.12 as go
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOBIN=/bin
ARG BUILDARCH=amd64
RUN go install github.com/go-delve/delve/cmd/dlv@v1.8.2
RUN go install github.com/grpc-ecosystem/grpc-health-probe@v0.4.22
ADD https://github.com/spiffe/spire/releases/download/v1.8.0/spire-1.8.0-linux-${BUILDARCH}-musl.tar.gz .
RUN tar xzvf spire-1.8.0-linux-${BUILDARCH}-musl.tar.gz -C /bin --strip=2 spire-1.8.0/bin/spire-server spire-1.8.0/bin/spire-agent

FROM go as build
WORKDIR /build
COPY go.mod go.sum ./
COPY . .
COPY ./local ./local
RUN go build -o /bin/forwarder .

FROM build as test
CMD go test -test.v ./...

FROM test as debug
CMD dlv -l :40000 --headless=true --api-version=2 test -test.v ./...

FROM alpine as runtime
COPY --from=build /bin/forwarder /bin/forwarder
COPY --from=build /bin/grpc-health-probe /bin/grpc-health-probe
RUN apk --update add supervisor \
                     openvswitch

# Create database and pid file directory
RUN /usr/bin/ovsdb-tool create /etc/openvswitch/conf.db
RUN mkdir -pv /var/run/openvswitch/

# Add configuration files
ADD build/supervisord.conf /etc/supervisord.conf
ADD build/configure-ovs.sh /usr/share/openvswitch/
RUN chmod 755 /usr/share/openvswitch/configure-ovs.sh

# When container starts, run supervisord process
ENTRYPOINT ["/bin/forwarder"]
