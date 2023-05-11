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
	req, err := http.ReadRequest(bufio.NewReader(conn))
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

	err = relay(remoteConn, conn)
	if err != nil {
		logger.Printf("Error relaying for %s: %s", remoteAddr, err)
		return
	}
	logger.Println("Done")
	// After a CONNECT, we don't expect another request. Close.
	return
}

/*
		if req.Method == "CONNECT" {
			// Handle HTTPS CONNECT
			//remoteConn, e := net.DialTimeout("tcp", net.JoinHostPort(req.URL.Hostname(), port), handshakeTimeout)
			remoteConn, err := net.DialTimeout("tcp", remoteAddr, handshakeTimeout)
			if err != nil {
				//TODO send error to client - 5xx?
				logger.Printf("Couldn't dial remote %s: %s", remoteAddr, err)
				return
			}
			defer remoteConn.Close()
			err = relay(remoteConn, conn)
			if err != nil {
				logger.Printf("Error relaying for %s: %s", remoteAddr, err)
				return
			}
			// After a CONNECT, we don't expect another request. Close.
			return
		}

		// If not CONNECT, original request was HTTP.
		// We're looping here in case client has multiple HTTP requests in one conn,
		// but our current handling still doesn't support things like streaming responses.

		// Assume  that this was browser -HTTP-> local proxy -HTTPS-front-> remote proxy (this)
		// Not CONNECT means it was originally HTTP, so use 80.
		//TODO replace DialTimeout with a Context!
		remoteConn, err := net.DialTimeout("tcp", remoteAddr, 5*time.Second)
		if err != nil {
			//TODO send error to client - 5xx?
			logger.Printf("Couldn't dial remote %s: %s", remoteAddr, err)
			continue
		}
		defer remoteConn.Close()
		logger.Printf("%v", req)
		err = req.Write(remoteConn)
		if err != nil {
			//TODO send error to client - Gateway Unavailable?
			logger.Printf("Couldn't forward to remote %s: %s", remoteAddr, err)
			continue
		}
		_, err = io.Copy(conn, remoteConn)
		if err != nil {
			logger.Printf("Couldn't forward response from %s to client: %s", remoteAddr, err)
			continue
		}
	}
}
*/

// TODO this needs a context with timeout and cancel
// TODO confirm this complies with spec!
// If one side closes, write whatever it sent to the other and quit.
func relay(dst, src net.Conn) error {
	errChan := make(chan error, 1)
	go func() {
		_, err := io.Copy(dst, src)
		errChan <- err
		dst.SetReadDeadline(time.Now().Add(5 * time.Second))
	}()
	_, err := io.Copy(src, dst)
	src.SetReadDeadline(time.Now().Add(5 * time.Second))
	err2 := <-errChan
	if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
		return err
	}
	if err2 != nil && !errors.Is(err2, os.ErrDeadlineExceeded) {
		return err2
	}
	return nil
}

/*
	// Check method
	if r.Method == "CONNECT" {
		//TODO handle context
		handleConnect(conn, r)
		return
	}

	// Not tunneling, so proxy HTTP
	host := r.Host
	logger.Printf("Method: %s Host: %s Path: %s",
		req.Method, req.Host, xhost, req.URL.Path)

	//logger.Printf("Request: %v\n", r.URL)
	//logger.Println(r.RequestURI)

	// Re-use incoming request
	// Replace localhost URL with host-based URL
	url, err := url.Parse(fmt.Sprintf("https://%s%s", frontDomain, r.URL))
	if err != nil {
		logger.Printf("url.Parse: %s", err)
		return
	}
	r.URL = url
	// Have to strip incoming RequestURI. Not allowed in client requests.
	r.RequestURI = ""

	//TODO update to use domain fronting
	// Currently we  enforce HTTPS
	//tcpAddr, _ := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:443", r.URL.Hostname()))
	//tcpAddr, _ := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:443", r.Host))
	//TODO update tls call to use actual hostname again
	//tcpAddr, _ := net.ResolveTCPAddr("tcp4", "xkcd.com:443")
	tcpAddr, _ := net.ResolveTCPAddr("tcp", frontDomain+":443")
	fmt.Printf("%v", tcpAddr)
	//remote, err := net.DialTCP("tcp",
	remote, err := tls.Dial("tcp",
		"xkcd.com:443",
		nil)
	if err != nil {
		logger.Printf("Couldn't dial remote %s: %s", r.URL.Hostname(), err)
		return
	}
	defer remote.Close()

	err = r.Write(remote)
	if err != nil {
		logger.Printf("Couldn't write to remote: %s", err)
		return
	}

	n, err := io.Copy(conn, remote)
	if err != nil {
		logger.Printf("Error during copy after %d bytes written: %s", n, err)
		return
	}
	logger.Printf("Read %d bytes", n)
}

func handleConnect(conn *net.TCPConn, r *http.Request) {
	//TODO
	return
}
*/
