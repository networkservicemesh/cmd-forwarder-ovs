FROM golang:1.16-buster as go
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOBIN=/bin
RUN go get github.com/go-delve/delve/cmd/dlv@v1.6.0
RUN go get github.com/edwarnicke/dl
RUN dl https://github.com/spiffe/spire/releases/download/v0.11.1/spire-0.11.1-linux-x86_64-glibc.tar.gz | \
    tar -xzvf - -C /bin --strip=3 ./spire-0.11.1/bin/spire-server ./spire-0.11.1/bin/spire-agent

FROM go as build
WORKDIR /build
COPY go.mod go.sum ./
COPY . .
RUN go build -o /bin/forwarder .

FROM build as test
CMD go test -test.v ./...

FROM test as debug
CMD dlv -l :40000 --headless=true --api-version=2 test -test.v ./...

FROM alpine as runtime
COPY --from=build /bin/forwarder /bin/forwarder
RUN apk --update add supervisor \
                     openvswitch

# Create database and pid file directory
RUN /usr/bin/ovsdb-tool create /etc/openvswitch/conf.db
RUN mkdir -pv /var/run/openvswitch/

# Add configuration files
ADD build/run_supervisord.sh /bin/run_supervisord.sh
ADD build/supervisord.conf /etc/supervisord.conf
ADD build/configure-ovs.sh /usr/share/openvswitch/
RUN chmod 755 /usr/share/openvswitch/configure-ovs.sh
RUN chmod +x /bin/run_supervisord.sh

# When container starts, run_supervisord.sh is executed
ENTRYPOINT ["/bin/run_supervisord.sh"]
