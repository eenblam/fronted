package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
	//"time"
)

// TODO arg for port
const port = 8080

// TODO make these variable, and make frontDomain plural
const frontDomain = "www.fastly.com"
const frontedHost = "fronted.site"

//TODO const remoteProxy = "remoteproxy.com"

func main() {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{
		// This is explicitly intended to run on localhost
		IP:   net.IPv4(127, 0, 0, 1),
		Port: port,
	})
	if err != nil {
		log.Fatalf("Could not listen on port %d: %s", port, err)
	}
	log.Printf("Listening on: %d", port)

	for {
		conn, err := l.AcceptTCP()
		if err != nil {
			log.Printf("Couldn't accept connection: %s", err)
			continue
		}
		log.Printf("Got conn from %s", conn.RemoteAddr().String())
		go handle(conn)
	}
}

const handshakeTimeout = 5 * time.Second

func handle(conn *net.TCPConn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(handshakeTimeout))

	logger := log.New(log.Writer(),
		fmt.Sprintf("[%s] ", conn.RemoteAddr().String()),
		log.Flags()|log.Lmsgprefix|log.Lshortfile)

	remoteAddr := net.JoinHostPort(frontDomain, "443")

	// Domain front the first request, then just pass subsequent requests directly on same conn
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		logger.Printf("Couldn't read request: %s", err)
	}
	conn.SetReadDeadline(time.Time{})

	logger.Printf("Method: %s Host: %s Path: %s",
		req.Method, req.Host, req.URL.Path)

	// Swap hosts
	dstHost, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		//TODO write error to client
		logger.Printf("Couldn't get host from req.Host: %s", err)
		return
	}
	req.Header.Add("X-Method", req.Method)
	req.Method = "GET"
	req.Header.Add("X-Host", dstHost)
	req.Host = frontedHost
	logger.Printf("New Host: %s", req.Host)

	// Have to strip incoming RequestURI. Not allowed in client requests.
	req.RequestURI = ""
	// Have to fix URL
	logger.Printf("URL before: %s", req.URL)
	//url, err := url.Parse(fmt.Sprintf("https://%s%s", frontDomain, req.URL))
	url, err := url.Parse(fmt.Sprintf("//%s:443", frontDomain))
	if err != nil {
		logger.Printf("url.Parse: %s", err)
		return
	}
	req.URL = url
	logger.Printf("URL after: %s", req.URL)

	req.Write(os.Stderr)
	//TODO how do I just use logger as an io.Writer?
	//req.Write(logger)

	//TODO not using handshakeTimeout
	keyFile := "tls-secrets-" + dstHost + ".txt"
	w, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		logger.Printf("Couldn't open %s: %s", keyFile, err)
		// Ensure we don't write to a bad handle
		w = nil
	}
	defer w.Close()
	config := &tls.Config{
		ServerName:   "www.fastly.com",
		KeyLogWriter: w, // DEBUG
	}
	remoteConn, err := tls.Dial("tcp", remoteAddr, config)
	//remoteConn, err := net.DialTimeout("tcp", remoteAddr, handshakeTimeout)
	if err != nil {
		//TODO send error to client - 5xx?
		logger.Printf("Couldn't dial remote %s: %s", remoteAddr, err)
		return
	}
	defer remoteConn.Close()
	logger.Printf("Connected to %s", remoteAddr)

	// Forward initial request
	err = req.Write(remoteConn)
	if err != nil {
		//TODO send error to client - Gateway Unavailable?
		logger.Printf("Couldn't forward to remote %s: %s", remoteAddr, err)
		return
	}

	// Read back request - along with extra headers from Fastly
	remoteReader := bufio.NewReader(remoteConn)
	resp, err := http.ReadResponse(remoteReader, req)
	if err != nil {
		logger.Printf("Couldn't read response: %s", err)
	}
	//TODO confirm 200 Connection established
	//TODO send client 200 Connection established
	_, err = fmt.Fprintf(conn, "%s 200 Connection established\r\n\r\n", req.Proto)
	if err != nil {
		logger.Printf("Couldn't send 200 to client: %s", err)
	}

	err = relay(remoteConn, conn, resp.Body)
	if err != nil {
		logger.Printf("Error relaying for %s: %s", remoteAddr, err)
		return
	}
	logger.Println("Done")
	// After a CONNECT, we don't expect another request. Close.
	return
}

// TODO this needs a context with timeout and cancel
// TODO confirm this complies with spec!
// If one side closes, write whatever it sent to the other and quit.
func relay(toRemote, local net.Conn, body io.ReadCloser) error {
	defer body.Close()
	errChan := make(chan error, 1)
	go func() {
		_, err := io.Copy(toRemote, local)
		errChan <- err
		toRemote.SetReadDeadline(time.Now().Add(5 * time.Second))
	}()
	_, err := io.Copy(local, body)
	local.SetReadDeadline(time.Now().Add(5 * time.Second))
	err2 := <-errChan
	if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
		return err
	}
	if err2 != nil && !errors.Is(err2, os.ErrDeadlineExceeded) {
		return err2
	}
	return nil
}
