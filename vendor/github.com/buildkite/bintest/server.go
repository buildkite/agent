package bintest

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// A single instance of the server is run for each golang process. The server has sessions which then
// have proxy calls within those sessions.

var (
	serverInstance *Server
	serverLock     deadlock.Mutex
)

// StartServer starts an instance of a proxy server
func StartServer() (*Server, error) {
	serverLock.Lock()
	defer serverLock.Unlock()

	if serverInstance == nil {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}

		s := &Server{
			Listener: l,
			URL:      "http://" + l.Addr().String(),
		}

		debugf("[server] Starting server on %s", s.URL)
		go func() {
			err = http.Serve(l, s)
			debugf("[server] Server on %s finished: %v", s.URL, err)
		}()

		serverInstance = s
	}

	return serverInstance, nil
}

// Stop the shared http server instance
func StopServer() error {
	serverLock.Lock()
	defer serverLock.Unlock()

	if serverInstance != nil {
		debugf("[server] Stopping server on %s", serverInstance.URL)
		_ = serverInstance.Close()
		serverInstance = nil
	}

	return nil
}

type Server struct {
	net.Listener
	URL string

	proxies      sync.Map
	callHandlers sync.Map
}

func (s *Server) registerProxy(p *Proxy) {
	debugf("[server] Registering proxy %s", p.Path)
	s.proxies.Store(p.Path, p)
}

func (s *Server) deregisterProxy(p *Proxy) {
	debugf("[server] Deregistering proxy %s", p.Path)
	s.proxies.Delete(p.Path)
}

var (
	callRouteRegex = regexp.MustCompile(`^/calls/(\d+)/(stdout|stderr|stdin|exitcode)$`)
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/debug" {
		body, _ := ioutil.ReadAll(r.Body)
		_ = r.Body.Close()
		debugf("%s", body)
		return
	}

	start := time.Now()
	debugf("[server] %s %s", r.Method, r.URL.Path)

	if r.URL.Path == `/calls/new` {
		s.handleNewCall(w, r)
		return
	}

	matches := callRouteRegex.FindStringSubmatch(r.URL.Path)

	if len(matches) == 0 {
		http.Error(w, "Unknown route "+r.URL.Path, http.StatusBadRequest)
		return
	}

	pid, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// dispatch the request to a handler with the given id
	handler, ok := s.callHandlers.Load(int(pid))
	if !ok {
		errorf("No call handler found for pid %d", pid)
		http.Error(w, "Unknown handler", http.StatusNotFound)
		return
	}

	debugf("[server] Found handler for %v", handler.(*callHandler).call.Args)

	handler.(*callHandler).ServeHTTP(w, r)
	debugf("[server] END %s (%v)", r.URL.Path, time.Now().Sub(start))
}

type callRequest struct {
	PID      int
	Args     []string
	Env      []string
	Dir      string
	HasStdin bool
}

func (s *Server) handleNewCall(w http.ResponseWriter, r *http.Request) {
	var req callRequest

	// parse the posted args end env
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// find the proxy instance in the server for the given path
	proxy, ok := s.proxies.Load(req.Args[0])
	if !ok {
		errorf("No bintest proxy registered that matches %q", req.Args[0])
		http.Error(w, "No bintest proxy registered that matches "+req.Args[0], http.StatusNotFound)
		return
	} else {
		debugf("[server] Found proxy for path %s", req.Args[0])
	}

	// these pipes connect the call to the various http request/responses
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	inR, inW := io.Pipe()

	// create a custom handler with the id for subsequent requests to hit
	call := proxy.(*Proxy).newCall(req.PID, req.Args, req.Env, req.Dir)
	call.Stdout = outW
	call.Stderr = errW
	call.Stdin = inR

	// close off stdin if it's not going to be provided
	if !req.HasStdin {
		_ = inW.Close()
	}

	// save the handler for subsequent requests
	s.callHandlers.Store(int(call.PID), &callHandler{
		call:   call,
		stdout: outR,
		stderr: errR,
		stdin:  inW,
	})

	debugf("[server] Registered call handler for pid %d", call.PID)

	// dispatch to whatever handles the call
	proxy.(*Proxy).Ch <- call
}

type callHandler struct {
	sync.WaitGroup
	call           *Call
	stdout, stderr *io.PipeReader
	stdin          *io.PipeWriter
}

func (ch *callHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch path.Base(r.URL.Path) {
	case "stdout":
		debugf("[server] Starting copy of stdout")
		copyPipeWithFlush(w, ch.stdout)
		debugf("[server] Finished copy of stdout")

	case "stderr":
		debugf("[server] Starting copy of stderr")
		copyPipeWithFlush(w, ch.stderr)
		debugf("[server] Finished copy of stderr")

	case "stdin":
		debugf("[server] Starting copy of stdin")
		_, _ = io.Copy(ch.stdin, r.Body)
		_ = r.Body.Close()
		_ = ch.stdin.Close()
		debugf("[server] Finished copy of stdin")

	case "exitcode":
		debugf("[server] Blocking on call for exitcode")
		exitCode := <-ch.call.exitCodeCh
		w.Header().Add("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(&exitCode)
		w.(http.Flusher).Flush()
		debugf("[server] Sending exit code %d to proxy", exitCode)
		ch.call.doneCh <- struct{}{}

	default:
		http.Error(w, "Unhandled request", http.StatusNotFound)
		return
	}
}

func copyPipeWithFlush(res http.ResponseWriter, pipeReader *io.PipeReader) {
	buffer := make([]byte, 1024)
	for {
		n, err := pipeReader.Read(buffer)
		if err != nil {
			_ = pipeReader.Close()
			break
		}

		data := buffer[0:n]
		_, _ = res.Write(data)

		if f, ok := res.(http.Flusher); ok {
			f.Flush()
		}

		// reset buffer
		for i := 0; i < n; i++ {
			buffer[i] = 0
		}
	}
}
