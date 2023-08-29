package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

// var addr = flag.String("listen", "127.0.0.1:8080", "listen address")
//var addr = flag.String("listen", "0.0.0.0:8080", "listen address")

//TODO could parse this from API at startup
// https://developer.fastly.com/reference/api/utils/public-ip-list/
// Current list: https://api.fastly.com/public-ip-list

var fastlyNetworks = getNetworks()

func getNetworks() []net.IPNet {
	networks := []string{
		"127.0.0.0/24", // localhost
		"23.235.32.0/20",
		"43.249.72.0/22",
		"103.244.50.0/24",
		"103.245.222.0/23",
		"103.245.224.0/24",
		"104.156.80.0/20",
		"140.248.64.0/18",
		"140.248.128.0/17",
		"146.75.0.0/17",
		"151.101.0.0/16",
		"157.52.64.0/18",
		"167.82.0.0/17",
		"167.82.128.0/20",
		"167.82.160.0/20",
		"167.82.224.0/20",
		"172.111.64.0/18",
		"185.31.16.0/22",
		"199.27.72.0/21",
		"199.232.0.0/16",
	}
	out := make([]net.IPNet, len(networks))
	for i, network := range networks {
		_, ipNet, err := net.ParseCIDR(network)
		if err != nil {
			log.Panicf("Couldn't parse network: %s", network)
		}
		out[i] = *ipNet
	}
	return out
}

func checkAddr(addr string) error {
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	maybeIP := net.ParseIP(ip)
	if maybeIP == nil {
		return fmt.Errorf("checkAddr() could not parse IP from %s", ip)
	}
	for _, network := range fastlyNetworks {
		if network.Contains(maybeIP) {
			return nil
		}
	}
	return fmt.Errorf("IP %s not in Fastly accept list", ip)
}

func main() {
	addr := flag.String("addr", ":8081", "(address):port")

	// To test locally, you can use -certfile cert.pem -keyfile key.pem
	defaultCert := "/etc/letsencrypt/live/backend.fronted.site/fullchain.pem"
	defaultKey := "/etc/letsencrypt/live/backend.fronted.site/privkey.pem"
	certFile := flag.String("certfile", defaultCert, "certificate PEM file")
	keyFile := flag.String("keyfile", defaultKey, "key PEM file")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	tlsCert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		panic(err)
	}
	config := &tls.Config{
		KeyLogWriter: nil, // This can be handy for debugging purposes
		Certificates: []tls.Certificate{tlsCert},
	}
	ln, err := tls.Listen("tcp", *addr, config)
	if err != nil {
		panic(err)
	}
	log.Printf("http proxy server listening %s\n", *addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Couldn't accept connection: %s", err)
			continue
		}
		go handle(conn)
	}
}

const handshakeTimeout = 5 * time.Second

func handle(conn net.Conn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(handshakeTimeout))

	logger := log.New(log.Writer(),
		fmt.Sprintf("[%s] ", conn.RemoteAddr().String()),
		log.Flags()|log.Lmsgprefix|log.Lshortfile)

	reader := bufio.NewReader(conn)
	for {
		//TODO properly handle HTTP/2 requests
		req, err := http.ReadRequest(reader)
		if err != nil {
			logger.Print("read request:", err)
			return
		}
		conn.SetReadDeadline(time.Time{})

		if err := checkAddr(conn.RemoteAddr().String()); err != nil {
			msg := fmt.Sprintf("%s 403\r\n\r\nI don't like you, %s\r\n\r\n", req.Proto, conn.RemoteAddr().String())
			logger.Printf(msg)
			conn.Write([]byte(msg))
			return
		}

		// Check if Host is:
		// backend.fronted.site: healthcheck
		// fronted.site: initial request proxied through fastly
		// TODO something else: subsequent HTTP request from pre-existing connection
		//                      not clear if Fastly will try to Host match these?
		if req.Host == "backend.fronted.site" && req.URL.Path == "/healthcheck.txt" && req.Method == "HEAD" {
			log.Printf("Got healthcheck from %s", conn.RemoteAddr().String())
			m := "%s 200\r\n\r\ncontent-length: 0\r\n\r\n"
			_, err = fmt.Fprintf(conn, m, req.Proto)
			if err != nil {
				logger.Printf("Couldn't write healthcheck reply: %s", err)
			}
			// Fastly requires we just close conn each time
			return
		}

		origHost, origMethod := req.Host, req.Method
		xhost := req.Header.Get("X-Host")
		// If X-Host, then update Host. Otherwise, just be a regular proxy.
		if xhost != "" {
			req.Host = xhost
		} else {
			// for now, just fail if no x-host
			logger.Printf("NOT FOUND: %s", xhost)
			_, _ = fmt.Fprintf(conn, "%s 404 Not found\r\n\r\n", req.Proto)
			return
		}
		xmethod := req.Header.Get("X-Method")
		if xmethod != "" {
			req.Method = xmethod
		}
		logger.Printf("Method: %s Host: %s X-Method: %s X-Host: %s URL: %s",
			origMethod, origHost, xmethod, xhost, req.URL.Path)
		//TODO fail if no X-Host.
		/*
			if xhost == "" {
				//TODO just reply 404
				logger.Printf("404 no X-Host for Host %s", req.Host)
				logger.Println(req)
				logger.Println(req.Header)
				_, err = fmt.Fprintf(conn, "%s 404 Not found\r\n\r\n", req.Proto)
				if err != nil {
					logger.Printf("Failed to write 404: %s", err)
				}
				// Just close connection
				return
			}
			req.Host = xhost
		*/

		if req.Method == "CONNECT" {
			// Handle HTTPS CONNECT
			//remoteConn, e := net.DialTimeout("tcp", net.JoinHostPort(req.URL.Hostname(), port), handshakeTimeout)
			remoteAddr := net.JoinHostPort(req.Host, "443")
			remoteConn, err := net.DialTimeout("tcp", remoteAddr, handshakeTimeout)
			if err != nil {
				//TODO send error to client - 5xx?
				logger.Printf("Couldn't dial remote %s: %s", remoteAddr, err)
				return
			}
			defer remoteConn.Close()
			_, err = fmt.Fprintf(conn, "%s 200 Connection established\r\n\r\n", req.Proto)
			if err != nil {
				//TODO send error to client - 5xx?
				logger.Printf("Couldn't connect to remote %s: %s", remoteAddr, err)
				return
			}
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
		remoteAddr := net.JoinHostPort(req.Host, "80")
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
