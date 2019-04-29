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
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestVNCHandler(t *testing.T) {
	testCase := func(url string, expectedStatus int, expectedAddr string, defhost string, defport uint16, allowHosts, allowPorts bool) func(*testing.T) {
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
				vnc := vncHandler(defhost, defport, false, allowHosts, allowPorts)
				m := mux.NewRouter()
				m.Handle("/vnc", vnc)
				m.Handle("/vnc/{host:[a-zA-Z0-9_.-]+}", vnc)
				m.Handle("/vnc/{host:[a-zA-Z0-9_.-]+}/{port:[0-9]+}", vnc)
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
	t.Run("Simple", testCase("http://example.com/vnc", 101, "localhost:5900", "localhost", 5900, false, false))
	t.Run("SimpleBlockHost", testCase("http://example.com/vnc/test", 401, "", "localhost", 5900, false, false))
	t.Run("SimpleBlockHostPort", testCase("http://example.com/vnc/test/1234", 401, "", "localhost", 5900, true, false))
	t.Run("Custom", testCase("http://example.com/vnc", 101, "example.com:1234", "example.com", 1234, false, false))
	t.Run("CustomHost", testCase("http://example.com/vnc/test", 101, "test:1234", "example.com", 1234, true, false))
	t.Run("CustomHostPort", testCase("http://example.com/vnc/test/3456", 101, "test:3456", "example.com", 1234, true, true))
}

func TestWebsockify(t *testing.T) {
	defer func() {
		if err := recover(); err != nil && !strings.Contains(fmt.Sprint(err), "not implemented") {
			panic(err)
		}
	}()
	websockify("google.com:80").ServeHTTP(nilResponseWriter{}, httptest.NewRequest("GET", "/", nil))
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
	r := httptest.NewRequest("GET", "http://example.com/go.mod", nil)
	w := httptest.NewRecorder()

	fs("zipfs", http.Dir(".")).ServeHTTP(w, r)

	buf, _ := ioutil.ReadAll(w.Result().Body)
	if !strings.Contains(string(buf), "github.com/spkg/zipfs") {
		if strings.Contains(string(buf), "github.com/geek1011/easy-novnc") {
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
