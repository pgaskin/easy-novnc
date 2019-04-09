# easy-novnc
An easy way to run a [noVNC](https://github.com/novnc/noVNC) instance and proxy with a single binary.

## Features
- Clean start page
- Optionally allow connections to arbitrary hosts (and ports)
- Single binary, no dependencies
- Easy setup

## Usage
```
Usage: easy-novnc [options]

Options:
  -a, --addr=":8080": The address to listen on
  -H, --arbitrary-hosts=false: Allow connection to other hosts
  -P, --arbitrary-ports=false: Allow connections to arbitrary ports (requires arbitraryHosts)
  -u, --basic-ui=false: Hide connection options from the main screen
      --help=false: Show this help text
  -h, --host="localhost": The host/ip to connect to by default
  -p, --port=5900: The port to connect to by default
  -v, --verbose=false: Show extra log info
```

## Updating
To update noVNC to the latest version from GitHub, run `go generate` or `go run novnc_generate.go`.