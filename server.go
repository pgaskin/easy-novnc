// +build !index_generate
// +build !novnc_generate

package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ogier/pflag"
	"golang.org/x/net/websocket"
)

//go:generate go run novnc_generate.go
//go:generate go run index_generate.go

func main() {
	pflag.Usage = func() {
		fmt.Printf("Usage: %s [options]\n\nOptions:\n", os.Args[0])
		pflag.PrintDefaults()
	}

	arbitraryHosts := pflag.BoolP("arbitrary-hosts", "H", false, "Allow connection to other hosts")
	arbitraryPorts := pflag.BoolP("arbitrary-ports", "P", false, "Allow connections to arbitrary ports (requires arbitraryHosts)")
	host := pflag.StringP("host", "h", "localhost", "The host/ip to connect to by default")
	port := pflag.Uint16P("port", "p", 5900, "The port to connect to by default")
	addr := pflag.StringP("addr", "a", ":8080", "The address to listen on")
	basicUI := pflag.BoolP("basic-ui", "u", false, "Hide connection options from the main screen")
	verbose := pflag.BoolP("verbose", "v", false, "Show extra log info")
	help := pflag.Bool("help", false, "Show this help text")
	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(1)
	}

	r := mux.NewRouter()
	r.Use(noCache)

	vnc := vncHandler(*host, *port, *verbose, *arbitraryHosts, *arbitraryPorts)
	r.Handle("/vnc", vnc)
	r.Handle("/vnc/{host:[a-zA-Z0-9_.-]+}", vnc)
	r.Handle("/vnc/{host:[a-zA-Z0-9_.-]+}/{port:[0-9]+}", vnc)

	r.NotFoundHandler = fs("noVNC-master", noVNC)
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		indexTMPL.Execute(w, map[string]interface{}{
			"arbitraryHosts": *arbitraryHosts,
			"arbitraryPorts": *arbitraryPorts,
			"host":           *host,
			"port":           *port,
			"addr":           *addr,
			"basicUI":        *basicUI,
		})
	})

	fmt.Printf("Listening on http://%s\n", *addr)
	if !*arbitraryHosts && !*arbitraryPorts && *host == "localhost" && *port == 5900 && !*basicUI {
		fmt.Printf("Run with --help for more options\n")
	}
	err := http.ListenAndServe(*addr, r)
	if err != nil {
		logf(true, "Error: %v\n", err)
		os.Exit(1)
	}
}

// vncHandler creates a handler for vnc connections. If host and port are set in
// the url vars, they will be used if allowed.
func vncHandler(defhost string, defport uint16, verbose, allowHosts, allowPorts bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var host, port string

		if host = mux.Vars(r)["host"]; host == "" {
			host = defhost
		} else if !allowHosts {
			logf(verbose, "connect %s disabled\n", host)
			http.Error(w, "--arbitrary-hosts disabled", http.StatusUnauthorized)
			return
		}

		if port = mux.Vars(r)["port"]; port == "" {
			port = fmt.Sprint(defport)
		} else if !allowPorts {
			logf(verbose, "connect %s:%s disabled\n", host, port)
			http.Error(w, "--arbitrary-ports disabled", http.StatusUnauthorized)
			return
		}

		logf(verbose, "connect %s:%s\n", host, port)
		websockify(host+":"+port).ServeHTTP(w, r)
	})
}

// logf calls fmt.Printf with the date if the condition is true.
func logf(cond bool, format string, a ...interface{}) {
	if cond {
		fmt.Printf("%s: %s", time.Now().Format("Jan 02 15:04:05"), fmt.Sprintf(format, a...))
	}
}

// noCache disables caching on a http.Handler.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// fs returns a http.Handler which serves a directory from a http.FileSystem.
func fs(dir string, fs http.FileSystem) http.Handler {
	return addPrefix("/"+strings.Trim(dir, "/"), http.FileServer(fs))
}

// addPrefix is similar to http.StripPrefix, except it adds a prefix.
func addPrefix(prefix string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = prefix + r.URL.Path
		h.ServeHTTP(w, r2)
	})
}

// websockify returns an http.Handler which proxies websocket requests to a tcp
// address.
func websockify(to string) http.Handler {
	return websocket.Server{
		Handshake: wsProxyHandshake,
		Handler:   wsProxyHandler(to),
	}
}

// wsProxyHandshake is a handshake handler for a websocket.Server.
func wsProxyHandshake(config *websocket.Config, r *http.Request) error {
	config.Protocol = []string{"binary"}
	r.Header.Set("Access-Control-Allow-Origin", "*")
	r.Header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
	return nil
}

// wsProxyHandler is a websocket.Handler which proxies to a tcp address.
func wsProxyHandler(to string) websocket.Handler {
	return func(ws *websocket.Conn) {
		conn, err := net.Dial("tcp", to)
		if err != nil {
			ws.Close()
			return
		}

		ws.PayloadType = websocket.BinaryFrame

		done := make(chan error)
		go copyCh(conn, ws, done)
		go copyCh(ws, conn, done)

		err = <-done
		if err != nil {
			fmt.Println(err)
		}

		conn.Close()
		ws.Close()
		<-done
	}
}

// copyCh is like io.Copy, but it writes to a channel when finished.
func copyCh(dst io.Writer, src io.Reader, done chan error) {
	_, err := io.Copy(dst, src)
	done <- err
}
