// +build !index_generate
// +build !novnc_generate

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/pflag"
	"golang.org/x/net/websocket"
)

//go:generate go run novnc_generate.go
//go:generate go run index_generate.go

// https://stackoverflow.com/a/17871737
var ipv6Regexp = `(?:(?:[0-9a-fA-F]{1,4}:){7,7}[0-9a-fA-F]{1,4}|(?:[0-9a-fA-F]{1,4}:){1,7}:|(?:[0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|(?:[0-9a-fA-F]{1,4}:){1,5}(?::[0-9a-fA-F]{1,4}){1,2}|(?:[0-9a-fA-F]{1,4}:){1,4}(?::[0-9a-fA-F]{1,4}){1,3}|(?:[0-9a-fA-F]{1,4}:){1,3}(?::[0-9a-fA-F]{1,4}){1,4}|(?:[0-9a-fA-F]{1,4}:){1,2}(?::[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:(?:(?::[0-9a-fA-F]{1,4}){1,6})|:(?:(?::[0-9a-fA-F]{1,4}){1,7}|:)|fe80:(?::[0-9a-fA-F]{0,4}){0,4}%[0-9a-zA-Z]{1,}|::(?:ffff(?::0{1,4}){0,1}:){0,1}(?:(?:25[0-5]|(?:2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(?:25[0-5]|(?:2[0-4]|1{0,1}[0-9]){0,1}[0-9])|(?:[0-9a-fA-F]{1,4}:){1,4}:(?:(?:25[0-5]|(?:2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(?:25[0-5]|(?:2[0-4]|1{0,1}[0-9]){0,1}[0-9]))`

func main() {
	pflag.Usage = func() {
		fmt.Printf("Usage: %s [options]\n\nOptions:\n", os.Args[0])
		pflag.PrintDefaults()
	}

	hostOptions := pflag.StringSliceP("host-option", "O", []string{}, "List of static hosts allowed to connect to (comma separated, name:host:port formatted)")
	arbitraryHosts := pflag.BoolP("arbitrary-hosts", "H", false, "Allow connection to other hosts")
	arbitraryPorts := pflag.BoolP("arbitrary-ports", "P", false, "Allow connections to arbitrary ports (requires arbitrary-hosts)")
	cidrWhitelist := pflag.StringSliceP("cidr-whitelist", "c", []string{}, "CIDR whitelist for when arbitrary hosts are enabled (comma separated) (conflicts with blacklist)")
	cidrBlacklist := pflag.StringSliceP("cidr-blacklist", "C", []string{}, "CIDR blacklist for when arbitrary hosts are enabled (comma separated) (conflicts with whitelist)")
	host := pflag.StringP("host", "h", "localhost", "The host/ip to connect to by default")
	port := pflag.Uint16P("port", "p", 5900, "The port to connect to by default")
	addr := pflag.StringP("addr", "a", ":8080", "The address to listen on")
	basicUI := pflag.BoolP("basic-ui", "u", false, "Hide connection options from the main screen")
	verbose := pflag.BoolP("verbose", "v", false, "Show extra log info")
	noURLPassword := pflag.Bool("no-url-password", false, "Do not allow password in URL params")
	novncParams := pflag.StringSlice("novnc-params", nil, "Extra URL params for noVNC (advanced) (comma separated key-value pairs) (e.g. resize=remote)")
	defaultViewOnly := pflag.Bool("default-view-only", false, "Use view-only by default")
	help := pflag.Bool("help", false, "Show this help text")

	envmap := map[string]string{
		"host-option":       "NOVNC_HOST_OPTION",
		"arbitrary-hosts":   "NOVNC_ARBITRARY_HOSTS",
		"arbitrary-ports":   "NOVNC_ARBITRARY_PORTS",
		"cidr-whitelist":    "NOVNC_CIDR_WHITELIST",
		"cidr-blacklist":    "NOVNC_CIDR_BLACKLIST",
		"host":              "NOVNC_HOST",
		"port":              "NOVNC_PORT",
		"addr":              "NOVNC_ADDR",
		"basic-ui":          "NOVNC_BASIC_UI",
		"no-url-password":   "NOVNC_NO_URL_PASSWORD",
		"novnc-params":      "NOVNC_PARAMS",
		"default-view-only": "NOVNC_DEFAULT_VIEW_ONLY",
		"verbose":           "NOVNC_VERBOSE",
	}

	if val, ok := os.LookupEnv("PORT"); ok {
		val = ":" + val
		fmt.Printf("Setting --addr from PORT to %#v\n", val)
		if err := pflag.Set("addr", val); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(2)
		}
	}

	pflag.VisitAll(func(flag *pflag.Flag) {
		if env, ok := envmap[flag.Name]; ok {
			flag.Usage += fmt.Sprintf(" (env %s)", env)
			if val, ok := os.LookupEnv(env); ok {
				fmt.Printf("Setting --%s from %s to %#v\n", flag.Name, env, val)
				if err := flag.Value.Set(val); err != nil {
					fmt.Printf("Error: %v\n", err)
					os.Exit(2)
				}
			}
		}
	})

	pflag.Parse()

	if *arbitraryPorts && !*arbitraryHosts {
		fmt.Printf("Error: arbitrary-ports requires arbitrary-hosts to be enabled.\n")
		os.Exit(2)
	}

	cidrList, isWhitelist, err := parseCIDRBlackWhiteList(*cidrBlacklist, *cidrWhitelist)
	if err != nil {
		fmt.Printf("Error: error parsing cidr blacklist/whitelist: %v.\n", err)
		os.Exit(2)
	}

	if len(cidrList) != 0 {
		if err := checkCIDRBlackWhiteListHost(*host, cidrList, isWhitelist); err != nil {
			fmt.Printf("Warning: default host does not parse cidr blacklist/whitelist: %v.\n", err)
		}
	}

	novncParamsMap := map[string]string{
		"resize": "scale",
	}
	for _, p := range *novncParams {
		spl := strings.SplitN(p, "=", 2)
		if len(spl) != 2 {
			fmt.Printf("Error: error parsing noVNC params: must be in key=value format.\n")
			os.Exit(2)
		}

		// https://github.com/novnc/noVNC/blob/master/docs/EMBEDDING.md
		switch spl[0] {
		case "resize", "logging", "repeaterID", "reconnect_delay", "view_clip":
			novncParamsMap[spl[0]] = spl[1]
		case "encrypt", "reconnect", "path", "password", "view_only", "show_dot", "bell", "autoconnect":
			fmt.Printf("Error: error parsing noVNC params: option %#v reserved for use by easy-novnc.\n", spl[0])
			os.Exit(2)
		default:
			fmt.Printf("Error: error parsing noVNC params: unknown option %#v.\n", spl[0])
			os.Exit(2)
		}
	}

	if *help {
		pflag.Usage()
		os.Exit(1)
	}

	r := mux.NewRouter()
	r.Use(noCache)
	r.Use(serverHeader)

	vnc := vncHandler(*host, *port, *verbose, *arbitraryHosts, *arbitraryPorts, parseHostOptions(*hostOptions), cidrList, isWhitelist)
	r.Handle("/vnc", vnc)
	r.Handle("/vnc/{host:[a-zA-Z0-9_.-]+}", vnc)
	r.Handle("/vnc/{host:[a-zA-Z0-9_.-]+}/{port:[0-9]+}", vnc)
	r.Handle("/vnc/{host:"+ipv6Regexp+"}", vnc)
	r.Handle("/vnc/{host:"+ipv6Regexp+"}/{port:[0-9]+}", vnc)

	r.NotFoundHandler = fs("noVNC-master", noVNC)
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		err := indexTMPL.Execute(w, map[string]interface{}{
			"hostOptions":     parseHostOptions(*hostOptions),
			"arbitraryHosts":  *arbitraryHosts,
			"arbitraryPorts":  *arbitraryPorts,
			"host":            *host,
			"port":            *port,
			"addr":            *addr,
			"basicUI":         *basicUI,
			"noURLPassword":   *noURLPassword,
			"defaultViewOnly": *defaultViewOnly,
			"params":          novncParamsMap,
		})

		if err != nil {
			logf(true, "Error: %v.\n", err)
		}
	})

	fmt.Printf("Listening on http://%s\n", *addr)
	if !*arbitraryHosts && !*arbitraryPorts && *host == "localhost" && *port == 5900 && !*basicUI {
		fmt.Printf("Run with --help for more options\n")
	}
	if err := http.ListenAndServe(*addr, r); err != nil {
		logf(true, "Error: %v.\n", err)
		os.Exit(1)
	}
}

// vncHandler creates a handler for vnc connections. If host and port are set in
// the url vars, they will be used if allowed.
func vncHandler(defhost string, defport uint16, verbose, allowHosts, allowPorts bool, hostOptions []hostOption, cidrList []*net.IPNet, isWhitelist bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var host, port string

		if host = mux.Vars(r)["host"]; host == "" {
			host = defhost
		} else if invalidOptionHost(hostOptions, host) {
			logf(verbose, "connect %s disabled\n", host)
			http.Error(w, "host is not part of options", http.StatusUnauthorized)
			return
		} else if !allowHosts && len(hostOptions) == 0 {
			logf(verbose, "connect %s disabled\n", host)
			http.Error(w, "--arbitrary-hosts disabled", http.StatusUnauthorized)
			return
		}

		if port = mux.Vars(r)["port"]; port == "" {
			port = fmt.Sprint(defport)
		} else if invalidOptionPort(hostOptions, port) {
			logf(verbose, "connect %s:%s disabled\n", host, port)
			http.Error(w, "port is not part of options", http.StatusUnauthorized)
			return
		} else if !allowPorts && len(hostOptions) == 0 {
			logf(verbose, "connect %s:%s disabled\n", host, port)
			http.Error(w, "--arbitrary-ports disabled", http.StatusUnauthorized)
			return
		}

		if len(cidrList) != 0 {
			if err := checkCIDRBlackWhiteListHost(host, cidrList, isWhitelist); err != nil {
				logf(verbose, "connect %s:%s not allowed: %v\n", host, port, err)
				http.Error(w, fmt.Sprintf("connect %s:%s not allowed: %v\n", host, port, err), http.StatusUnauthorized)
				return
			}
		}

		addr := host + ":" + port
		if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
			addr = "[" + host + "]:" + port
		}

		logf(verbose, "connect %s\n", addr)
		w.Header().Set("X-Target-Addr", addr)
		websockify(addr, []byte("RFB")).ServeHTTP(w, r)
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

// serverHeader sets the Server header for a http.Handler.
func serverHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "easy-novnc")
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
// address and checks magic bytes.
func websockify(to string, magic []byte) http.Handler {
	return websocket.Server{
		Handshake: wsProxyHandshake,
		Handler:   wsProxyHandler(to, magic),
	}
}

// wsProxyHandshake is a handshake handler for a websocket.Server.
func wsProxyHandshake(config *websocket.Config, r *http.Request) error {
	if r.Header.Get("Sec-WebSocket-Protocol") != "" {
		config.Protocol = []string{"binary"}
	}
	r.Header.Set("Access-Control-Allow-Origin", "*")
	r.Header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
	return nil
}

// wsProxyHandler is a websocket.Handler which proxies to a tcp address with a
// magic byte check.
func wsProxyHandler(to string, magic []byte) websocket.Handler {
	return func(ws *websocket.Conn) {
		conn, err := net.Dial("tcp", to)
		if err != nil {
			ws.Close()
			return
		}

		ws.PayloadType = websocket.BinaryFrame

		m := newMagicCheck(conn, magic)

		done := make(chan error)
		go copyCh(conn, ws, done)
		go copyCh(ws, m, done)

		err = <-done
		if m.Failed() {
			logf(true, "attempt to connect to non-VNC port (%s, %#v)\n", to, string(m.Magic()))
		} else if err != nil {
			logf(true, "%v\n", err)
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

// checkCIDRBlackWhiteListHost checks the provided host/ip against a blacklist/whitelist.
func checkCIDRBlackWhiteListHost(host string, cidrList []*net.IPNet, isWhitelist bool) error {
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if err := checkCIDRBlackWhiteList(ip, cidrList, isWhitelist); err != nil {
			return err
		}
	}
	return nil
}

// checkCIDRBlackWhiteList checks an IP against a blacklist/whitelist.
func checkCIDRBlackWhiteList(ip net.IP, cidrList []*net.IPNet, isWhitelist bool) error {
	var matchedCIDR *net.IPNet
	for _, cidr := range cidrList {
		if cidr.Contains(ip) {
			matchedCIDR = cidr
			break
		}
	}
	if matchedCIDR == nil && isWhitelist {
		return fmt.Errorf("ip %s does not match any whitelisted cidr", ip)
	} else if matchedCIDR != nil && !isWhitelist {
		return fmt.Errorf("ip %s matches blacklisted cidr %s", ip, matchedCIDR)
	}
	return nil
}

// parseCIDRBlackWhiteList returns either a parsed blacklist or whitelist of
// CIDRs. If neither is specified, isWhitelist is false and the slice is empty.
func parseCIDRBlackWhiteList(blacklist []string, whitelist []string) (cidrs []*net.IPNet, isWhitelist bool, err error) {
	if len(blacklist) != 0 && len(whitelist) != 0 {
		err = errors.New("only one of blacklist/whitelist can be specified")
		return
	}
	if len(whitelist) != 0 {
		isWhitelist = true
		cidrs, err = parseCIDRList(whitelist)
	} else {
		cidrs, err = parseCIDRList(blacklist)
	}
	return
}

// parseCIDRList parses a list of CIDRs.
func parseCIDRList(cidrs []string) ([]*net.IPNet, error) {
	res := make([]*net.IPNet, len(cidrs))
	for i, str := range cidrs {
		_, cidr, err := net.ParseCIDR(str)
		if err != nil {
			return nil, fmt.Errorf("error parsing CIDR '%s': %v", str, err)
		}
		res[i] = cidr
	}
	return res, nil
}

// magicCheck implements an efficient wrapper around an io.Reader which checks
// for magic bytes at the beginning, and will return a sticky io.EOF and stop
// reading from the original reader as soon as a mismatch starts.
type magicCheck struct {
	rdr io.Reader
	exp []byte
	len int
	rem int
	act []byte
	fld bool
}

func newMagicCheck(r io.Reader, magic []byte) *magicCheck {
	return &magicCheck{r, magic, len(magic), len(magic), make([]byte, len(magic)), false}
}

// Failed returns true if the magic check has failed (note that it returns false
// if the source io.Reader reached io.EOF before the check was complete).
func (m *magicCheck) Failed() bool {
	return m.fld
}

// Magic returns the magic which was read so far.
func (m *magicCheck) Magic() []byte {
	return m.act
}

func (m *magicCheck) Read(buf []byte) (n int, err error) {
	if m.fld {
		return 0, io.EOF
	}
	n, err = m.rdr.Read(buf)
	if err == nil && n > 0 && m.rem > 0 {
		m.rem -= copy(m.act[m.len-m.rem:], buf[:n])
		for i := 0; i < m.len-m.rem; i++ {
			if m.act[i] != m.exp[i] {
				m.fld = true
				return 0, io.EOF
			}
		}
	}
	return n, err
}

type hostOption struct {
	Name string
	Host string
	Port string
}

func parseHostOptions(options []string) []hostOption {
	result := make([]hostOption, 0)

	for _, row := range options {
		option := strings.SplitN(row, ":", 3)

		result = append(result, hostOption{
			Name: option[0],
			Host: option[1],
			Port: option[2],
		})
	}

	return result
}

func invalidOptionHost(options []hostOption, host string) bool {
	for _, o := range options {
		if o.Host == host {
			return false
		}
	}

	return true
}

func invalidOptionPort(options []hostOption, port string) bool {
	for _, o := range options {
		if o.Port == port {
			return false
		}
	}

	return true
}
