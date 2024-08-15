# Build

## Build cmd binary locally

You can build the locally by executing

```bash
go build ./...
```

## Build Docker container

You can build the docker container by running:

```bash
docker build .
```

# Usage

## Envorinment config

* `NSM_NAME`                      - Name of Endpoint (default: "forwarder")
* `NSM_LABELS`                    - Labels related to this forwarder-vpp instance (default: "p2p:true")
* `NSM_NSNAME`                    - Name of Network Service to Register with Registry (default: "forwarder")
* `NSM_BRIDGE_NAME`               - Name of the OvS bridge (default: "br-nsm")
* `NSM_TUNNEL_IP`                 - IP or CIDR to use for tunnels
* `NSM_VXLAN_PORT`                - VXLAN port to use (default: "4789")
* `NSM_CONNECT_TO`                - url to connect to (default: "unix:///connect.to.socket")
* `NSM_DIAL_TIMEOUT`              - Timeout for the dial the next endpoint (default: "50ms")
* `NSM_MAX_TOKEN_LIFETIME`        - maximum lifetime of tokens (default: "24h")
* `NSM_REGISTRY_CLIENT_POLICIES`  - paths to files and directories that contain registry client policies (default: "etc/nsm/opa/common/.*.rego,etc/nsm/opa/registry/.*.rego,etc/nsm/opa/client/.*.rego")
* `NSM_RESOURCE_POLL_TIMEOUT`     - device plugin polling timeout (default: "30s")
* `NSM_DEVICE_PLUGIN_PATH`        - path to the device plugin directory (default: "/var/lib/kubelet/device-plugins/")
* `NSM_POD_RESOURCES_PATH`        - path to the pod resources directory (default: "/var/lib/kubelet/pod-resources/")
* `NSM_SRIOV_CONFIG_FILE`         - PCI resources config path (default: "pci.config")
* `NSM_L2_RESOURCE_SELECTOR_FILE` - config file for resource to label matching
* `NSM_PCI_DEVICES_PATH`          - path to the PCI devices directory (default: "/sys/bus/pci/devices")
* `NSM_PCI_DRIVERS_PATH`          - path to the PCI drivers directory (default: "/sys/bus/pci/drivers")
* `NSM_CGROUP_PATH`               - path to the host cgroup directory (default: "/host/sys/fs/cgroup/devices")
* `NSM_VFIO_PATH`                 - path to the host VFIO directory (default: "/host/dev/vfio")
* `NSM_LOG_LEVEL`                 - Log level (default: "INFO")
* `NSM_OPEN_TELEMETRY_ENDPOINT`   - OpenTelemetry Collector Endpoint (default: "otel-collector.observability.svc.cluster.local:4317")
* `NSM_METRICS_EXPORT_INTERVAL`   - interval between mertics exports (default: "10s")
* `NSM_PPROF_ENABLED`             - is pprof enabled (default: "false")
* `NSM_PPROF_LISTEN_ON`           - pprof URL to ListenAndServe (default: "localhost:6060")

# Testing

## Testing Docker container

Testing is run via a Docker container.  To run testing run:

```bash
docker run --privileged --rm $(docker build -q --target test .)
```

# Debugging

## Debugging the tests
If you wish to debug the test code itself, that can be acheived by running:

```bash
docker run --privileged --rm -p 40000:40000 $(docker build -q --target debug .)
```

This will result in the tests running under dlv.  Connecting your debugger to localhost:40000 will allow you to debug.

```bash
-p 40000:40000
```
forwards port 40000 in the container to localhost:40000 where you can attach with your debugger.

```bash
--target debug
```

Runs the debug target, which is just like the test target, but starts tests with dlv listening on port 40000 inside the container.

## Debugging the cmd

When you run 'cmd' you will see an early line of output that tells you:

```Setting env variable DLV_LISTEN_FORWARDER to a valid dlv '--listen' value will cause the dlv debugger to execute this binary and listen as directed.```

If you follow those instructions when running the Docker container:
```bash
docker run --privileged -e DLV_LISTEN_FORWARDER=:50000 -p 50000:50000 --rm $(docker build -q --target test .)
```

```-e DLV_LISTEN_FORWARDER=:50000``` tells docker to set the environment variable DLV_LISTEN_FORWARDER to :50000 telling
dlv to listen on port 50000.

```-p 50000:50000``` tells docker to forward port 50000 in the container to port 50000 in the host.  From there, you can
just connect dlv using your favorite IDE and debug cmd.

## Debugging the tests and the cmd

```bash
docker run --privileged -e DLV_LISTEN_FORWARDER=:50000 -p 40000:40000 -p 50000:50000 --rm $(docker build -q --target debug .)
```

Please note, the tests **start** the cmd, so until you connect to port 40000 with your debugger and walk the tests
through to the point of running cmd, you will not be able to attach a debugger on port 50000 to the cmd.
