# wstcp
Tunnels local VNC connections over TCP to an easy-novnc server over WebSockets.

```
Usage: wstcp [options] proxy_host [target_host [target_port]]

Options:
      --help            Show this help text
  -l, --listen string   Address to listen for connections on (default ":5900")


Arguments:
  proxy_host    The easy-novnc server in the format [http[s]://]hostname[:port].
                If the protocol isn't specified, it is autodetected.
  target_host   The target address to connect to. Requires --arbitrary-hosts to
                be set on the server.
  target_port   The target port to connect to. Requires --arbitrary-ports to be
                set on the server.
```
