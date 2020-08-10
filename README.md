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

### Launching LCA Allocator
LCA Allocator is located in the directory `allocator` and is responsible for allocating new server instances. This must be launched on every machine that you would like to be part of the pool to automatically allocate applications on.
```
$ cd allocator
$ ./allocator &
Spawning LCA Allocator
Setting stream handlers
Creating DHT
Connecting to bootstrap nodes...
Connected to bootstrap node: {12D3KooWGegi4bWDPw9f6x2mZ6zxtsjR8w4ax1tEMDKCNqdYBt7X: [/ip4/10.11.17.32/tcp/4001]}
Connected to bootstrap node: {QmeZvvPZgrpgSLFyTYwCUEbyK6Ks8Cjm2GGrP2PA78zjAk: [/ip4/10.11.17.15/tcp/4001]}
Connected to 3 peers!
Creating Routing Discovery
Finished setting up libp2p Node with PID QmSd2hr7EXmQYVrmtA3PzqK3xhbuKGzTZcaAsMQzWJkkcJ and Multiaddresses [/ip4/127.0.0.1/tcp/42440 /ip4/10.11.17.10/tcp/42440 /ip6/::1/tcp/46571]
Waiting for requests...
```

### Launching Proxy in Server Mode
In order for an application instance to access the system, a Proxy instance is needed. Server mode is where the Proxy binds to an instance of an application registered in Hash Lookup and allows the instance to be found and find other instances to query via the LCA Manager. All HTTP requests must be routed through the IP address and port of Proxy instance with the requested application as the first section and the arguments in the second, ie. HTTP GET http://127.0.0.1/hello-world-server/hello.
```
$ cd proxy
$ ./proxy 1234 hello-world-server 10.11.17.10
Starting LCA Manager in service mode with arguments hello-world-server 10.11.17.10:8080
Setting stream handlers
Creating DHT
Connecting to bootstrap nodes...
Connected to bootstrap node: {12D3KooWGegi4bWDPw9f6x2mZ6zxtsjR8w4ax1tEMDKCNqdYBt7X: [/ip4/10.11.17.32/tcp/4001]}
Connected to bootstrap node: {QmeZvvPZgrpgSLFyTYwCUEbyK6Ks8Cjm2GGrP2PA78zjAk: [/ip4/10.11.17.15/tcp/4001]}
Connected to 5 peers!
Creating Routing Discovery
Finished setting up libp2p Node with PID QmPwKBtQ4zgF3HsSU6itot5WLMSBZcuZLNVgb1PNRDY245 and Multiaddresses [/ip4/127.0.0.1/tcp/40588 /ip4/10.11.17.10/tcp/40588 /ip6/::1/tcp/45279]
Connecting to: {QmWTs3McLrTjuAdNsEoh7J8uYa2ZDbWsBkg8UV2t2Jn1s1: [/ip4/10.11.17.13/tcp/46042 /ip6/::1/tcp/35435 /ip4/127.0.0.1/tcp/46042]}
Setting stream handlers
Creating DHT
Connecting to bootstrap nodes...
Connected to bootstrap node: {12D3KooWGegi4bWDPw9f6x2mZ6zxtsjR8w4ax1tEMDKCNqdYBt7X: [/ip4/10.11.17.32/tcp/4001]}
Connected to bootstrap node: {QmeZvvPZgrpgSLFyTYwCUEbyK6Ks8Cjm2GGrP2PA78zjAk: [/ip4/10.11.17.15/tcp/4001]}
Connected to 2 peers!
Creating Routing Discovery
Finished setting up libp2p Node with PID Qmb6envkTd1fPWaj5ysTjGPjAvYMCjdFw3gMQYFf6zRGhS and Multiaddresses [/ip4/127.0.0.1/tcp/38168 /ip4/10.11.17.10/tcp/38168 /ip6/::1/tcp/44438]
Launching proxy PeerCache instance
Setting performance requirements based on perf.conf soft limit: 100ms hard limit: 1s
Starting HTTP Proxy on 127.0.0.1:1234
Launching cache update function
```

### Launching in Client Mode (Anonymous Mode)
In order for an appliation instance to access the system, a Proxy instance is needed. Client mode is where the Proxy serves an application that does not have HTTP API routes and accessess the system only to make requests. Such an application does not need to be registered with Hash Lookup. The Proxy instance allows the application instance to find other instances to query via the LCA Manager. All HTTP requests must be routed through the IP address and port of Proxy instance with the requested application as the first section and the arguments in the second, ie. HTTP GET http://127.0.0.1/hello-world-server/hello.
```
$ cd proxy
$ ./proxy 1234
Starting LCA Manager in service mode with arguments hello-world-server 10.11.17.10:8080
Setting stream handlers
Creating DHT
Connecting to bootstrap nodes...
Connected to bootstrap node: {12D3KooWGegi4bWDPw9f6x2mZ6zxtsjR8w4ax1tEMDKCNqdYBt7X: [/ip4/10.11.17.32/tcp/4001]}
Connected to bootstrap node: {QmeZvvPZgrpgSLFyTYwCUEbyK6Ks8Cjm2GGrP2PA78zjAk: [/ip4/10.11.17.15/tcp/4001]}
Connected to 5 peers!
Creating Routing Discovery
Finished setting up libp2p Node with PID QmPwKBtQ4zgF3HsSU6itot5WLMSBZcuZLNVgb1PNRDY245 and Multiaddresses [/ip4/127.0.0.1/tcp/40588 /ip4/10.11.17.10/tcp/40588 /ip6/::1/tcp/45279]
Connecting to: {QmWTs3McLrTjuAdNsEoh7J8uYa2ZDbWsBkg8UV2t2Jn1s1: [/ip4/10.11.17.13/tcp/46042 /ip6/::1/tcp/35435 /ip4/127.0.0.1/tcp/46042]}
Setting stream handlers
Creating DHT
Connecting to bootstrap nodes...
Connected to bootstrap node: {12D3KooWGegi4bWDPw9f6x2mZ6zxtsjR8w4ax1tEMDKCNqdYBt7X: [/ip4/10.11.17.32/tcp/4001]}
Connected to bootstrap node: {QmeZvvPZgrpgSLFyTYwCUEbyK6Ks8Cjm2GGrP2PA78zjAk: [/ip4/10.11.17.15/tcp/4001]}
Connected to 2 peers!
Creating Routing Discovery
Finished setting up libp2p Node with PID Qmb6envkTd1fPWaj5ysTjGPjAvYMCjdFw3gMQYFf6zRGhS and Multiaddresses [/ip4/127.0.0.1/tcp/38168 /ip4/10.11.17.10/tcp/38168 /ip6/::1/tcp/44438]
Launching proxy PeerCache instance
Setting performance requirements based on perf.conf soft limit: 100ms hard limit: 1s
Starting HTTP Proxy on 127.0.0.1:1234
Launching cache update function
```

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
