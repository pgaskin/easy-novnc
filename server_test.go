package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestVNCHandler(t *testing.T) {
	testCase := func(url string, expectedStatus int, expectedAddr string, defhost string, defport uint16, allowHosts, allowPorts bool, hostOptions []hostOption, cidrList []*net.IPNet, isWhitelist bool) func(*testing.T) {
		return func(t *testing.T) {
			r := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()

			var ws bool
			func() {
				defer func() {
					// workaround for websocket library issue with a fake http response
					if err := recover(); strings.Contains(fmt.Sprint(err), "not http.Hijacker") {
						ws = true
					} else if err != nil {
						panic(err)
					}
				}()
				vnc := vncHandler(defhost, defport, false, allowHosts, allowPorts, hostOptions, cidrList, isWhitelist)
				m := mux.NewRouter()
				m.Handle("/vnc", vnc)
				m.Handle("/vnc/{host:[a-zA-Z0-9_.-]+}", vnc)
				m.Handle("/vnc/{host:[a-zA-Z0-9_.-]+}/{port:[0-9]+}", vnc)
				m.Handle("/vnc/{host:"+ipv6Regexp+"}", vnc)
				m.Handle("/vnc/{host:"+ipv6Regexp+"}/{port:[0-9]+}", vnc)
				m.ServeHTTP(w, r)
			}()

			c := w.Result().StatusCode
			if ws && c == 200 {
				c = 101
			}
			if c != expectedStatus {
				t.Errorf("expected status %d, got %d", expectedStatus, c)
			}

			if a := w.Result().Header.Get("X-Target-Addr"); a != expectedAddr {
				t.Errorf("expected addr %#v, got %#v", expectedAddr, a)
			}
		}
	}

	emptyHostOptions := make([]hostOption, 0)
	t.Run("Simple", testCase("http://example.com/vnc", 101, "localhost:5900", "localhost", 5900, false, false, emptyHostOptions, nil, false))
	t.Run("SimpleBlockHost", testCase("http://example.com/vnc/test", 401, "", "localhost", 5900, false, false, emptyHostOptions, nil, false))
	t.Run("SimpleBlockHostPort", testCase("http://example.com/vnc/test/1234", 401, "", "localhost", 5900, true, false, emptyHostOptions, nil, false))

	t.Run("Custom", testCase("http://example.com/vnc", 101, "example.com:1234", "example.com", 1234, false, false, emptyHostOptions, nil, false))
	t.Run("CustomHost", testCase("http://example.com/vnc/test", 101, "test:1234", "example.com", 1234, true, false, emptyHostOptions, nil, false))
	t.Run("CustomHostPort", testCase("http://example.com/vnc/test/3456", 101, "test:3456", "example.com", 1234, true, true, emptyHostOptions, nil, false))

	simpleHostOption := append(emptyHostOptions, hostOption{
		Name: "dummy",
		Host: "hostoption",
		Port: "5900",
	})
	t.Run("SingleHostOption", testCase("http://example.com/vnc/hostoption/5900", 101, "hostoption:5900", "example.com", 1234, true, true, simpleHostOption, nil, false))

	t.Run("CIDRWhitelistAllowIP", testCase("http://example.com/vnc/10.0.0.1", 101, "10.0.0.1:5900", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("192.168.0.0/24,10.0.0.0/24"), true))
	t.Run("CIDRWhitelistBlockIP", testCase("http://example.com/vnc/127.0.0.1", 401, "", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("192.168.0.0/24,10.0.0.0/24"), true))
	t.Run("CIDRBlacklistBlockIP", testCase("http://example.com/vnc/10.0.0.1", 401, "", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("192.168.0.0/24,10.0.0.0/24"), false))
	t.Run("CIDRBlacklistAllowIP", testCase("http://example.com/vnc/127.0.0.1/5900", 101, "127.0.0.1:5900", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("192.168.0.0/24,10.0.0.0/24"), false))

	t.Run("CIDRWhitelistAllowHost", testCase("http://example.com/vnc/10.0.0.1.ip.dns.geek1011.net", 101, "10.0.0.1.ip.dns.geek1011.net:5900", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("192.168.0.0/24,10.0.0.0/24"), true))
	t.Run("CIDRWhitelistBlockHost", testCase("http://example.com/vnc/127.0.0.1.ip.dns.geek1011.net", 401, "", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("192.168.0.0/24,10.0.0.0/24"), true))
	t.Run("CIDRBlacklistBlockHost", testCase("http://example.com/vnc/10.0.0.1.ip.dns.geek1011.net", 401, "", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("192.168.0.0/24,10.0.0.0/24"), false))
	t.Run("CIDRBlacklistAllowHost", testCase("http://example.com/vnc/127.0.0.1.ip.dns.geek1011.net/5900", 101, "127.0.0.1.ip.dns.geek1011.net:5900", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("192.168.0.0/24,10.0.0.0/24"), false))

	t.Run("CIDRWhitelistAllowIPv6", testCase("http://example.com/vnc/a%3Ab%3Ac%3Ad%3Aa%3Ab%3Ac%3Ad", 101, "[a:b:c:d:a:b:c:d]:5900", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("a:b:c:d:a:b:c:d/120"), true))
	t.Run("CIDRWhitelistBlockIPv6", testCase("http://example.com/vnc/a%3Ab%3Ac%3Ad%3Aa%3Ab%3Ad%3Ad", 401, "", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("a:b:c:d:a:b:c:d/120"), true))
	t.Run("CIDRBlacklistBlockIPv6", testCase("http://example.com/vnc/a%3Ab%3Ac%3Ad%3Aa%3Ab%3Ac%3Ad", 401, "", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("a:b:c:d:a:b:c:d/120"), false))
	t.Run("CIDRBlacklistAllowIPv6", testCase("http://example.com/vnc/a%3Ab%3Ac%3Ad%3Aa%3Ab%3Ad%3Ad/5900", 101, "[a:b:c:d:a:b:d:d]:5900", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("a:b:c:d:a:b:c:d/120"), false))

	t.Run("CIDRWhitelistAllowHostv6", testCase("http://example.com/vnc/a.b.c.d.a.b.c.d.ip.dns.geek1011.net", 101, "a.b.c.d.a.b.c.d.ip.dns.geek1011.net:5900", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("a:b:c:d:a:b:c:d/120"), true))
	t.Run("CIDRWhitelistBlockHostv6", testCase("http://example.com/vnc/a.b.c.d.a.b.d.d.ip.dns.geek1011.net", 401, "", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("a:b:c:d:a:b:c:d/120"), true))
	t.Run("CIDRBlacklistBlockHostv6", testCase("http://example.com/vnc/a.b.c.d.a.b.c.d.ip.dns.geek1011.net", 401, "", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("a:b:c:d:a:b:c:d/120"), false))
	t.Run("CIDRBlacklistAllowHostv6", testCase("http://example.com/vnc/a.b.c.d.a.b.d.d.ip.dns.geek1011.net/5900", 101, "a.b.c.d.a.b.d.d.ip.dns.geek1011.net:5900", "localhost", 5900, true, true, emptyHostOptions, mustParseCIDRList("a:b:c:d:a:b:c:d/120"), false))
}

func TestWebsockify(t *testing.T) {
	defer func() {
		if err := recover(); err != nil && !strings.Contains(fmt.Sprint(err), "not implemented") {
			panic(err)
		}
	}()
	websockify("google.com:80", []byte(nil)).ServeHTTP(nilResponseWriter{}, httptest.NewRequest("GET", "/", nil))
	// TODO: proper testing
}

type nilResponseWriter struct{}

func (nilResponseWriter) Write(buf []byte) (int, error) {
	return len(buf), nil
}
func (nilResponseWriter) WriteHeader(int) {}
func (nilResponseWriter) Header() http.Header {
	return http.Header{}
}
func (nilResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("not implemented")
}

func TestLogf(t *testing.T) {
	for _, c := range []struct {
		Cond   bool
		Format string
		Args   []interface{}
		Out    string
	}{
		{false, "test\n", nil, ""},
		{true, "test\n", nil, "test"},
		{true, "test %s\n", []interface{}{"test"}, "test test"},
	} {
		logf(c.Cond, c.Format, c.Args...)
		// TODO: figure out a way to test c.Out
	}
}

func TestNoCache(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/go.mod", nil)
	w := httptest.NewRecorder()

	noCache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	})).ServeHTTP(w, r)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("wrong Cache-Control header: %#v", cc)
	}
}

func TestServerHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/go.mod", nil)
	w := httptest.NewRecorder()

	serverHeader(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	})).ServeHTTP(w, r)

	if cc := w.Result().Header.Get("Server"); cc != "easy-novnc" {
		t.Errorf("wrong Server header: %#v", cc)
	}
}

func TestFS(t *testing.T) {
	d, err := ioutil.TempDir("", "easy-novnc")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(d)

	err = ioutil.WriteFile(filepath.Join(d, "test.txt"), []byte("foo"), 0644)
	if err != nil {
		panic(err)
	}

	err = os.Mkdir(filepath.Join(d, "tmp"), 0755)
	if err != nil {
		panic(err)
	}

	err = ioutil.WriteFile(filepath.Join(d, "tmp", "test.txt"), []byte("foobar"), 0644)
	if err != nil {
		panic(err)
	}

	r := httptest.NewRequest("GET", "http://example.com/test.txt", nil)
	w := httptest.NewRecorder()

	fs("tmp", http.Dir(d)).ServeHTTP(w, r)

	buf, _ := ioutil.ReadAll(w.Result().Body)
	if !strings.Contains(string(buf), "foo") {
		if !strings.Contains(string(buf), "foobar") {
			t.Errorf("serving from wrong subdir, got %#v", string(buf))
		}
		t.Errorf("wrong response, got %#v", string(buf))
	}
}

func TestAddPrefix(t *testing.T) {
	for _, c := range [][]string{
		{"", "http://example.com/", "http://example.com/"},
		{"prefix", "http://example.com/", "http://example.com/prefix/"},
		{"prefix", "http://example.com/test", "http://example.com/prefix/test"},
		{"prefix", "http://example.com/test/", "http://example.com/prefix/test/"},
		{"prefix/prefix1", "http://example.com/test/", "http://example.com/prefix/prefix1/test/"},
		{"prefix", "/test/", "prefix/test/"},
	} {
		r := httptest.NewRequest("GET", c[1], nil)
		w := httptest.NewRecorder()

		addPrefix(c[0], http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, r.URL.String())
		})).ServeHTTP(w, r)

		buf, _ := ioutil.ReadAll(w.Result().Body)
		if string(buf) != c[2] {
			t.Errorf("expected %#v for addPrefix %#v to %#v, got %#v", c[2], c[0], c[1], string(buf))
		}
	}
}

func TestCopyCh(t *testing.T) {
	testCase := func(r *testReader, shouldError bool) func(*testing.T) {
		return func(t *testing.T) {
			dst := new(bytes.Buffer)
			src := r
			ch := make(chan error)

			go copyCh(dst, src, ch)
			n := time.Now()

			select {
			case err := <-ch:
				if !shouldError && err != nil {
					t.Errorf("unexpected error: %v", err)
				} else if shouldError && err == nil {
					t.Errorf("expected error")
				}
				if time.Now().Sub(n) < r.MinTime() {
					t.Errorf("returned too fast")
				}
			case <-time.After(time.Second):
				t.Errorf("error channel not written to")
			}
		}
	}
	t.Run("NoError", testCase(&testReader{5, time.Millisecond * 50, 0, 0}, false))
	t.Run("Error", testCase(&testReader{5, time.Millisecond * 50, 2, 0}, true))
}

func TestCIDRBlackWhiteList(t *testing.T) {
	testCase := func(cidrList []*net.IPNet, isWhitelist bool, hosts []string, shouldFail bool) func(t *testing.T) {
		return func(t *testing.T) {
			for _, host := range hosts {
				err := checkCIDRBlackWhiteListHost(host, cidrList, isWhitelist)
				if err == nil && shouldFail {
					t.Errorf("expected %s to fail test for cidr list (isWhitelist=%t) %s", host, isWhitelist, cidrList)
				} else if err != nil && !shouldFail {
					t.Errorf("expected %s not to fail test for cidr list (isWhitelist=%t) %s", host, isWhitelist, cidrList)
				}
			}
		}
	}
	t.Run("WhitelistAllow", testCase(mustParseCIDRList("10.0.0.0/24,127.0.0.0/16"), true, []string{"10.0.0.1", "127.0.1.1", "10.0.0.9.ip.dns.geek1011.net"}, false))
	t.Run("WhitelistBlock", testCase(mustParseCIDRList("10.0.0.0/24,127.0.0.0/16"), true, []string{"11.0.0.1", "1.0.1.1", "1.2.3.4.ip.dns.geek1011.net"}, true))
	t.Run("BlacklistAllow", testCase(mustParseCIDRList("10.0.0.0/24,127.0.0.0/16"), false, []string{"11.0.0.1", "1.0.1.1", "1.2.3.4.ip.dns.geek1011.net"}, false))
	t.Run("BlacklistBlock", testCase(mustParseCIDRList("10.0.0.0/24,127.0.0.0/16"), false, []string{"10.0.0.1", "127.0.1.1", "10.0.0.9.ip.dns.geek1011.net"}, true))
	t.Run("WhitelistAllowv6", testCase(mustParseCIDRList("a:b:c:d:a:b:c:d/120"), true, []string{"a:b:c:d:a:b:c:d", "a:b:c:d:a:b:c:a", "a.b.c.d.a.b.c.d.ip.dns.geek1011.net"}, false))
	t.Run("WhitelistBlockv6", testCase(mustParseCIDRList("a:b:c:d:a:b:c:d/120"), true, []string{"a:b:c:d:a:b:d:d", "a:b:c:d:a:b:d:a", "a.b.c.d.a.b.d.d.ip.dns.geek1011.net"}, true))
	t.Run("BlacklistAllowv6", testCase(mustParseCIDRList("a:b:c:d:a:b:c:d/120"), false, []string{"a:b:c:d:a:b:d:d", "a:b:c:d:a:b:d:a", "a.b.c.d.a.b.d.d.ip.dns.geek1011.net"}, false))
	t.Run("BlacklistBlockv6", testCase(mustParseCIDRList("a:b:c:d:a:b:c:d/120"), false, []string{"a:b:c:d:a:b:c:d", "a:b:c:d:a:b:c:a", "a.b.c.d.a.b.c.d.ip.dns.geek1011.net"}, true))
}

func TestParseCIDRList(t *testing.T) {
	strs := []string{
		"127.0.0.0/16",
		"192.168.0.0/24",
		"a:b:c:d:a:b:c:0/120",
	}
	cidrs, err := parseCIDRList(strs)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}
	for i, expected := range strs {
		if actual := cidrs[i].String(); expected != actual {
			t.Errorf("expected cidr %s at index %d, got %s", expected, i, actual)
		}
	}

	strs = []string{
		"127.0.0.0/16",
		"192.168.0.0.123.4/24",
		"a:b:c:d:a:b:c:d/120",
	}
	_, err = parseCIDRList(strs)
	if err == nil {
		t.Errorf("expected error: when parsing erroneous list")
	}
}

func TestIPv6Regexp(t *testing.T) {
	re := regexp.MustCompile(ipv6Regexp)
	for _, ipv6 := range []string{
		"1:2:3:4:5:6:7:8",
		"1::",
		"1:2:3:4:5:6:7::",
		"1::8",
		"1:2:3:4:5:6::8",
		"1:2:3:4:5:6::8",
		"1::7:8",
		"1:2:3:4:5::7:8",
		"1:2:3:4:5::8",
		"1::5:6:7:8",
		"1:2:3::5:6:7:8",
		"1:2:3::8",
		"1::4:5:6:7:8",
		"1:2::4:5:6:7:8",
		"1:2::8",
		"::2:3:4:5:6:7:8",
		"::2:3:4:5:6:7:8",
		"::8",
		"::",
		"fe80::7:8%eth0",
		"fe80::7:8%1",
		"::255.255.255.255",
		"::ffff:255.255.255.255",
		"::ffff:0:255.255.255.255",
		"2001:db8:3:4::192.0.2.33",
		"64:ff9b::192.0.2.33",
	} {
		if !re.MatchString(ipv6) {
			t.Errorf("expected regexp to match %#v", ipv6)
		}
	}
}

func TestMagicCheck(t *testing.T) {
	for _, tc := range []struct {
		Name string

		Magic []byte
		Input []byte

		EOFAt  int
		Failed bool
	}{
		{"Good_BothEmpty", []byte(""), []byte(""), 0, false},
		{"Good_EmptyMagicWithInput", []byte(""), []byte(" "), 1, false},
		{"Good_EmptyInputWithMagic", []byte("RFB"), []byte(""), 0, false},
		{"Good_ExactMatch", []byte("RFB"), []byte("RFB"), 3, false},
		{"Good_ExactMatchWithExtra", []byte("RFB"), []byte("RFB 005.000"), 11, false},
		{"Bad_NoMatch", []byte("RFB"), []byte("..."), 0, true},
		{"Bad_PartialMatch", []byte("RFB"), []byte("R.."), 1, true},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			m := newMagicCheck(bytes.NewReader(tc.Input), tc.Magic)
			var buf []byte

			rbuf := make([]byte, 1)
			for {
				n, err := m.Read(rbuf)
				if err == io.EOF {
					if n, err := m.Read(rbuf); err != io.EOF || n != 0 {
						t.Errorf("expected io.EOF to stick with no bytes read")
					}
					if tc.EOFAt < 0 {
						t.Errorf("unexpected eof after %d bytes", len(buf))
					} else if len(buf) != tc.EOFAt {
						t.Errorf("unexpected eof after %d bytes, expected %d bytes (buf: %s)", len(buf), tc.EOFAt, string(buf))
					} else if m.Failed() != tc.Failed {
						t.Errorf("expected failed=%t, got %t", tc.Failed, m.Failed())
					} else if !m.Failed() && len(tc.Input) >= len(tc.Magic) && !bytes.Equal(m.Magic(), tc.Magic) {
						t.Errorf("shouldn't have passed the magic check: %s != %s", string(m.Magic()), string(tc.Magic))
					}
					break
				} else if err != nil {
					panic(err)
				} else if n > 0 {
					buf = append(buf, rbuf[:n]...)
				}
			}
		})
	}
}

// testReader is a custom io.Reader which throttles the reads and can return
// an error at a specific point.
type testReader struct {
	N     int
	Delay time.Duration
	Errn  int
	v     int
}

func (t *testReader) Read(buf []byte) (int, error) {
	if t.v >= t.N {
		return 0, io.EOF
	}

	t.v++
	time.Sleep(t.Delay)

	if t.Errn == t.v {
		return 1, errors.New("test error")
	}

	buf[0] = 0xFF
	return 1, nil
}

func (t *testReader) MinTime() time.Duration {
	if t.Errn < t.N {
		return t.Delay * time.Duration(t.Errn)
	}
	return t.Delay * time.Duration(t.N)
}

func mustParseCIDRList(str string) []*net.IPNet {
	cidrs, err := parseCIDRList(strings.Split(str, ","))
	if err != nil {
		panic(err)
	}
	return cidrs
}
