# TCP Proxy

The TCP proxy functions via a two-stage process:
1) Client queries proxy to open TCP endpoint to a service
    - Querying can be done via HTTP (for now). Query format will be similar to currently existing proxy where the path segment of the URL specifies the service.
    - Proxy resolves the service name to hash ID and connects to the service (or service's proxy once #36 is done), while opening a new listening socket and port for the requesting client.
    - The proxy returns the port number it has opened just for the client in the HTTP response.
2) Client can then connect to the proxy via the newly opened port, which acts as a TCP proxy to the service.

In the long run, must also consider how to specify a potential chain of services.
