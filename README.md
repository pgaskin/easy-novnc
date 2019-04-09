# easy-novnc
An easy way to run a [noVNC](https://github.com/novnc/noVNC) instance and proxy with a single binary.

[![Deploy](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy)

## Features
- Clean start page
- Optionally allow connections to arbitrary hosts (and ports)
- Single binary, no dependencies
- Easy setup

## Usage
```
Usage: easy-novnc [options]

Options:
  -a, --addr string       The address to listen on (env NOVNC_ADDR) (default ":8080")
  -H, --arbitrary-hosts   Allow connection to other hosts (env NOVNC_ARBITRARY_HOSTS)
  -P, --arbitrary-ports   Allow connections to arbitrary ports (requires arbitraryHosts) (env NOVNC_ARBITRARY_PORTS)
  -u, --basic-ui          Hide connection options from the main screen (env NOVNC_BASIC_UI)
      --help              Show this help text
  -h, --host string       The host/ip to connect to by default (env NOVNC_HOST) (default "localhost")
  -p, --port uint16       The port to connect to by default (env NOVNC_PORT) (default 5900)
      --no-url-password   Do not allow password in URL params (env NOVNC_NO_URL_PASSWORD)
  -v, --verbose           Show extra log info (env NOVNC_VERBOSE)
```

## Updating
To update noVNC to the latest version from GitHub, run `go generate` or `go run novnc_generate.go`.