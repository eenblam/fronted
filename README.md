# fronted
fronted is a multi-layer HTTP proxy.
The first layer (`client/`) is a proxy meant to run on localhost,
which handles requests from the user's browser.
The second layer is a CDN that allows domain fronting; the best example I know of in 2023 is Fastly.
The third layer (`server/`) is a proxy server behind a CDN that allows domain fronting.

The client layer handles the domain fronting concerns:
* It makes a connection to https://fastly.com but sets the Host header to `fronted.site`, a domain I control.
* It smuggles the destination `Host` header as a custom `X-Host` header.
* It smuggles the desired HTTP method as a custom `X-Method` header, since the CONNECT method might be disallowed.

The server layer resolves the `X-*` headers above to recover the original request from the browser,
then attempts to function as a normal proxy:
CONNECT requests work as expected, as do other methods (to allow HTTP-only proxying.)

**This is just a fun exploratory project.
Please don't look here for a reliable censorship circumvention method.**

## What stops this from working as a `CONNECT` proxy?
Originally, I thought it would be neat to provide an HTTP CONNECT proxy
that would block resistance via domain fronting.
The only popular edge provider I know of that currently allows domain fronting is Fastly.
However, Fastly doesn't allow CONNECT, so I tried smuggling the method through.

That is, whenever the local proxy client saw a CONNECT request for `destination.site`:

* the client rewrote the request as a GET and set `Host: fronted.site` (my backend)
* the client set two custom headers: `X-Host: destination.site` and `X-Method: CONNECT`
* the client forwarded it to https://fastly.com 
* Fastly terminated SSL for fastly.com, read `Host: fronted.site`, and forwarded the request to my backend.
* My backend (the proxy server) read `X-Method` and `X-Host`.
    * For `X-Method: CONNECT`, it would establish a connection to `destination.site` (the value of `X-Host`).
* After a successful connection to `destination.site`, the proxy server would reply to the proxy client (via Fastly) with `HTTP/1.1 200 Connection established` and begin forwarding bytes in each direction.

That allowed me to smuggle CONNECT requests through Fastly!
Everything sent to the client after that `Connection established` message should be data from `destination.site`.
However, this all broke since Fastly (a Varnish server) sets additional response headers
after the initial `Connection established`!

So, where the proxy client (and ultimately the browser) expected a bytestream, it instead saw this:

```
HTTP/1.1 200 Connection established
Connection: keep-alive
Accept-Ranges: bytes
Date: Thu, 11 May 2023 23:29:06 GMT
Via: 1.1 varnish
X-Served-By: cache-bur-kbur8200134-BUR
X-Cache: MISS
X-Cache-Hits: 0
X-Timer: S1683847747.605387,VS0,VE29
transfer-encoding: chunked
```

It's possible that I can edit the Varnish config used by Fastly to remove some of these,
but at the very least `transfer-encoding` is [protected](https://developer.fastly.com/reference/http/http-headers/Transfer-Encoding/)
from modification in config, so smuggling the CONNECT is a no-go.

## But can we just discard the headers?
Note that RFC 9110 states:

> A server MUST NOT send any Transfer-Encoding or Content-Length header fields in a 2xx (Successful) response to CONNECT. A client MUST ignore any Content-Length or Transfer-Encoding header fields received in a successful response to CONNECT.

This means that, while the response from Fastly is technically invalid,
the client should nonetheless ignore those two response headers.
There's no reason the client couldn't *also* ignore the other headers provided by Fastly in this case,
and only forward the first header line to the browser before proxying bytes.

There are still two issues, though.
First, the bytestream from the server is now encapsulated [Chunked Transfer Coding](https://www.rfc-editor.org/rfc/rfc9112#chunked.encoding).
This isn't really a problem, though:
we can just read chunks and forward the contents as bytes.

However, the weirder problem is that we're diving into some rough edges of
[RFC 9112 Section 2.2](https://www.rfc-editor.org/rfc/rfc9112#name-message-parsing).
In particular, we *could* change our encapsulating request method to Fastly from GET to POST,
and then specify no `Content-Length` header.

I'm pretty sure this will happen:

* Fastly receives a POST with no `Content-Length`
* Fastly will decide the connection can't be persistent, and we'll lose that `Connection: keep-alive` response.

I'm not yet sure about the following without further testing:

* Fastly decides it has to read until the connection is closed, OR it sends a 400 Bad Request
* In the later case, we're out of luck. In the former, we should take a crack at implementing a chunk decoder!

