package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"golang.org/x/net/websocket"
)

func main() {
	retry := pflag.IntP("retry", "r", -1, "Interval (seconds) to retry initial connection on failure")
	listen := pflag.StringP("listen", "l", ":5900", "Address to listen for connections on")
	help := pflag.Bool("help", false, "Show this help text")
	pflag.Parse()

	if *help || pflag.NArg() < 1 || pflag.NArg() > 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] proxy_host [target_host [target_port]]\n\nOptions:\n", os.Args[0])
		pflag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n\nArguments:\n")
		fmt.Fprintf(os.Stderr, "  proxy_host    The easy-novnc server in the format [http[s]://]hostname[:port].\n                If the protocol isn't specified, it is autodetected.\n")
		fmt.Fprintf(os.Stderr, "  target_host   The target address to connect to. Requires --arbitrary-hosts to\n                be set on the server.\n")
		fmt.Fprintf(os.Stderr, "  target_port   The target port to connect to. Requires --arbitrary-ports to be\n                set on the server.\n")
		os.Exit(2)
	}

	host, addr, port := pflag.Arg(0), pflag.Arg(1), pflag.Arg(2)

	var url string
	for {
		host, err := detect(host, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			if *retry >= 0 {
				fmt.Fprintf(os.Stderr, "Retrying after %d seconds...\n", *retry)
				time.Sleep(time.Second * time.Duration(*retry))
				continue
			}
			os.Exit(1)
		}

		url = host + "/vnc"
		if addr != "" {
			url += "/" + addr
			if port != "" {
				url += "/" + port
			}
		}

		fmt.Printf("Testing connection to %s.\n", url)
		if err := check(url); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			if *retry >= 0 {
				fmt.Fprintf(os.Stderr, "Retrying after %d seconds...\n", *retry)
				time.Sleep(time.Second * time.Duration(*retry))
				continue
			}
			os.Exit(1)
		}

		break
	}

	fmt.Printf("Listening %s => %s.\n", *listen, url)
	if err := wstun(*listen, url); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func detect(host string, verbose bool) (string, error) {
	if strings.Contains(host, "://") {
		if verbose {
			fmt.Printf("Testing connection to %s.\n", host)
		}
		resp, err := (&http.Client{Timeout: time.Second}).Get(host)
		if err != nil {
			return "", err
		} else if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("unexpected status %s", resp.Status)
		}
		resp.Body.Close()
		return host, nil
	}

	if verbose {
		fmt.Printf("No protocol specified, autodetecting.\n")
	}

	c := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != via[len(via)-1].URL.Scheme {
				return http.ErrUseLastResponse
			}
			return nil
		},
		Timeout: time.Second,
	}

	var err error
	orig := host
	for _, proto := range []string{"https", "http"} {
		host = fmt.Sprintf("%s://%s", proto, orig)
		if verbose {
			fmt.Printf("... trying %s", host)
		}

		resp, herr := c.Get(host)
		if herr != nil {
			err = fmt.Errorf("proto %s: %v", proto, herr)
			if verbose {
				fmt.Printf(": %v\n", err)
			}
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("unexpected status %s", resp.Status)
			if verbose {
				fmt.Printf(": %v\n", err)
			}
			continue
		}

		if verbose {
			fmt.Printf(": ok\n")
		}
		return host, nil
	}
	return "", err
}

func check(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		buf, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("easy-novnc server: %s", string(buf))
	}
	return nil
}

func wstun(listen, target string) error {
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	var i int
	for {
		i++
		conn, err := listener.Accept()

		fmt.Printf("Accepted connection %d from %s\n", i, conn.RemoteAddr())
		if nerr, ok := err.(net.Error); ok && nerr.Temporary() {
			fmt.Printf("Warning: connection %d: temporary error: %v, trying again in 100ms\n", i, err)
			time.Sleep(time.Millisecond * 100)
			continue
		} else if err != nil {
			return err
		}

		go func(i int, conn net.Conn) {
			wsconn, err := websocket.Dial(strings.Replace(target, "http", "ws", 1), "binary", target)
			if err != nil {
				fmt.Printf("Warning: connection %d: dial target websocket: %v, closing connection\n", i, err)
				conn.Close()
				fmt.Printf("Connection %d closed\n", i)
				return
			}

			done := make(chan error)
			go copyCh(wsconn, conn, done)
			go copyCh(conn, wsconn, done)

			if err := <-done; err != nil {
				fmt.Printf("Warning: connection %d: %v, closing connection\n", i, err)
				wsconn.Close()
				conn.Close()
				fmt.Printf("Connection %d closed\n", i)
				return
			}

			wsconn.Close()
			conn.Close()
			<-done

			fmt.Printf("Connection %d closed\n", i)
		}(i, conn)
	}
}

func copyCh(dst io.Writer, src io.Reader, done chan error) {
	_, err := io.Copy(dst, src)
	done <- err
}
