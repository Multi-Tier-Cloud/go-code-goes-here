# service-manager

## Introduction
This repository houses Go applications that takes care of allocating application instances, discovering application instances, and dispatching requests. The primary components of the service manager system is the LCA Allocator and the Proxy. The LCA Allocator runs on every physical server and is responsible for servicing allocation requests from the LCA Manager. The Proxy is the entrypoint for clients and servers to find application instances and make HTTP requests. A LCA Manager instance runs inside every Proxy instance to handle finding and requesting application instances.

## Getting Started
This getting started guide assumes the required system components are already configured and running, namely [Registry Service](https://github.com/PhysarumSM/service-registry) and [Monitoring](https://github.com/PhysarumSM/monitoring). Refer to repository READMEs for more information. For a more general Getting Started guide, see the [main repository](https://github.com/PhysarumSM/PhysarumSM).

### Building service-manager
Build Requirements:
- Go 1.13
- bash shell

A bash script is included with the repository called `build.sh` that takes care of building the required Go source files. Go 1.13 must be installed as other Go versions are not supported. Run `build.sh` to build service-manager.

### Adding your Application to Docker and service-registry
Applications must be added to docker hub for use in Physarum. See [Registry Service](https://github.com/PhysarumSM/service-registry) for more information.

### Modifying the Config to point to your Bootstrap
The bootstraps in the PhysarumSM system are the monitoring nodes. Copy and paste the multiaddress of the monitoring nodes started as a prerequisite and add them to the `config/config.json` under `Bootstraps` to enable connection to the correct bootstrap node.

### Launching LCA Allocator
LCA Allocator is located in the directory `allocator` and is responsible for allocating new server instances. This must be launched on every machine that you would like to be part of the pool to automatically allocate applications on. Based on the config in `config/config.json` it will automatically connect to an appropriate bootstrap.
```
$ cd allocator
$ ./allocator &
```

### Launching Proxy in Server Mode
In order for an application instance to access other applications and be accessable to other applications through the system, a Proxy instance is needed. Server mode is used by an application that wants to be accessible by other applications. The Proxy binds to an instance of an application registered in `service-registry` and allows the instance to be found via the LCA Manager. All HTTP requests must be routed through the IP address and port of Proxy instance with the requested application as the first section and the arguments in the second, ie. HTTP GET http://127.0.0.1/hello-world-server/hello.
```
$ cd proxy
$ ./proxy 1234 hello-world-server 10.11.17.10
```
Run `$ proxy -help` for more commandline options

### Launching in Client Mode (Anonymous Mode)
In order for an application instance to access other applications and be accessable to other applications through the system, a Proxy instance is needed. Client mode is used by an application or user that does not need to be accessible to other applications. This also means that the connected application does not need to be registered with `service-registry`. The Client mode Proxy instance allows the application to find other application instances via the LCA Manager and queries them. All HTTP requests must be routed through the IP address and port of Proxy instance with the requested application as the first section and the arguments in the second, ie. HTTP GET http://127.0.0.1/hello-world-server/hello.
```
$ cd proxy
$ ./proxy 1234
```
Run `$ proxy -help` for more commandline options

## Advanced Usage

### Commandline Arguments
Refer to usage message when running applications for help.

### Custom Configuration
By default, LCA Allocator and Proxy read in a configuration file called `conf.json` in the `conf` directory. Default configuration is included with the repository from Github. In order to use custom configuration settings, modify `conf.json` or point LCA Allocator and Proxy to a different configuration file using the `--configfile` commandline flag.

#### Configuration Format
```json
{
    "Perf": {
        "SoftReq": {
            "RTT": int(milliseconds)
        },
        "HardReq": {
            "RTT": int(milliseconds)
        }
    },
    "Bootstraps": [
        string(multiaddress)
    ]
}
```

#### Configuration Fields
Field | Description
---|---
Perf | Soft and hard performance requirements in terms of milliseconds
Bootstraps | List of bootstrap multiaddresses to connect to on startup

## System-Level Description
Coming soon
