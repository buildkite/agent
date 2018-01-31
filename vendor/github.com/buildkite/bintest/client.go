package bintest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

type Client struct {
	Debug bool
	URL   string

	Args []string
	Dir  string
	Env  []string
	PID  int

	Stdin  io.ReadCloser
	Stdout io.WriteCloser
	Stderr io.WriteCloser
}

func NewClient(URL string) *Client {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	return &Client{
		URL:    URL,
		Args:   os.Args,
		Env:    os.Environ(),
		Dir:    wd,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		PID:    os.Getpid(),
	}
}

func NewClientFromEnv() *Client {
	server := os.Getenv(ServerEnvVar)
	if server == `` {
		panic(fmt.Sprintf("No %s environment var set", ServerEnvVar))
	}
	return NewClient(server)
}

// Run the client, panics on error and returns an exit code on success
func (c *Client) Run() int {
	c.debugf("Running %s", strings.Join(c.Args, " "))

	var req = callRequest{
		PID:      c.PID,
		Args:     c.Args,
		Env:      c.Env,
		Dir:      c.Dir,
		HasStdin: c.isStdinReadable(),
	}

	// Fire off an initial request to start the flow
	if err := c.postJSON(c.URL+`/calls/new`, req); err != nil {
		c.debugf("Error from server: %v", err)
		panic(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	if c.isStdinReadable() {
		go func() {
			r, w := io.Pipe()
			wg.Add(1)

			go func() {
				defer wg.Done()
				c.debugf("Copying from Stdin")
				_, err := io.Copy(w, os.Stdin)
				if err != nil {
					c.debugf("Error copying from stdin: %v", err)
					_ = w.CloseWithError(err)
					return
				}
				_ = w.Close()
				c.debugf("Done copying from Stdin")
			}()

			stdinReq, stdinErr := http.NewRequest("POST", fmt.Sprintf("%s/calls/%d/stdin", c.URL, req.PID), r)
			if stdinErr != nil {
				panic(stdinErr)
			}

			resp, err := http.DefaultClient.Do(stdinReq)
			if err != nil {
				panic(err)
			}

			if resp.StatusCode != http.StatusOK {
				panic(fmt.Errorf(
					"Request to %s failed: %s",
					resp.Request.URL.String(),
					resp.Status))
			}
		}()
	} else if c.Stdin != nil {
		c.debugf("Closing stdin, nothing to read")
		_ = c.Stdin.Close()
	} else {
		c.debugf("No stdin, skipping")
	}

	go func() {
		c.debugf("Reading stdout")
		err := c.getStream(fmt.Sprintf("/calls/%d/stdout", req.PID), c.Stdout, &wg)
		if err != nil {
			panic(err)
		}
	}()

	go func() {
		c.debugf("Reading stderr")
		err := c.getStream(fmt.Sprintf("/calls/%d/stderr", req.PID), c.Stderr, &wg)
		if err != nil {
			panic(err)
		}
	}()

	c.debugf("Waiting for streams to finish")
	wg.Wait()
	c.debugf("Streams finished, waiting for exit code")

	exitCodeResp, err := http.Get(fmt.Sprintf("%s/calls/%d/exitcode", c.URL, req.PID))
	if err != nil {
		panic(err)
	}

	var exitCode int
	if err = json.NewDecoder(exitCodeResp.Body).Decode(&exitCode); err != nil {
		panic(err)
	}

	c.debugf("Got an exit code of %d", exitCode)
	return exitCode
}

func (c *Client) isStdinReadable() bool {
	if c.Stdin == nil {
		c.debugf("Nil stdin passed")
		return false
	}

	// check that we have a named pipe with stuff to read
	// See https://stackoverflow.com/a/26567513
	if stdinFile, ok := c.Stdin.(*os.File); ok {
		stat, _ := stdinFile.Stat()
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			c.debugf("Stdin is a terminal, nothing to read")
			return false
		}

		if stat.Size() > 0 {
			c.debugf("Stdin has %d bytes to read", stat.Size())
			return true
		}
	}

	return true
}

func (c *Client) debugf(pattern string, args ...interface{}) {
	if c.Debug {
		format := fmt.Sprintf("[client %d] %s", c.PID, pattern)

		b := bytes.NewBufferString(fmt.Sprintf(format, args...))
		u := c.URL + "/debug"

		resp, err := http.Post(u, "text/plain; charset=utf-8", b)
		if err != nil {
			log.Printf("Error posting to debug: %v", err)
		} else {
			_ = resp.Body.Close()
		}
	}
}

func (c *Client) get(path string) (*http.Response, error) {
	resp, err := http.Get(c.URL + path)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"Request to %s failed: %s",
			resp.Request.URL.String(),
			resp.Status)
	}

	return resp, err
}

func (c *Client) getStream(path string, w io.WriteCloser, wg *sync.WaitGroup) error {
	resp, err := c.get(path)
	if err != nil {
		return err
	}

	go func() {
		c.debugf("Copying from %s", path)
		b, _ := io.Copy(w, resp.Body)
		_ = resp.Body.Close()
		wg.Done()
		c.debugf("Copied %d bytes from %s", b, path)
	}()

	return nil
}

func (c *Client) postJSON(url string, from interface{}) (err error) {
	body := new(bytes.Buffer)
	if err = json.NewEncoder(body).Encode(from); err != nil {
		return err
	}

	resp, respErr := http.Post(url, "application/json; charset=utf-8", body)
	if respErr != nil {
		return err
	}
	defer func() {
		if respErr := resp.Body.Close(); respErr != nil {
			err = respErr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"Request to %s failed: %s",
			resp.Request.URL.String(),
			resp.Status)
	}

	return nil
}
