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

## Why not a `CONNECT` proxy?
Originally, I thought it would be neat to provide an HTTP CONNECT proxy
that would block resistance via domain fronting.
The only popular edge provider I know of at the moment
that allows domain fronting is Fastly.
However, Fastly doesn't allow CONNECT,
so I tried smuggling methods through.

That is, whenever the local proxy client saw a CONNECT request for `destination.site`:

* the client rewrote the request as a GET and set `Host: fronted.site` (my backend)
* the client set two custom headers: `X-Host: destination.site` and `X-Method: CONNECT`
* the client forwarded it to https://fastly.com 
* Fastly terminated SSL for fastly.com, read `Host: fronted.site`, and forwarded the request to my backend.
* My backend (the proxy server) read `X-Method` and `X-Host`. For `X-Method: CONNECT`, it would establish a connection to `destination.site` (the value of `X-Host`).
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
