# fronted
fronted is a two-layer HTTP proxy.
The first layer is a proxy meant to run on localhost, to service a browser.
The second layer is a proxy behind a CDN that allows domain fronting.

The first layer (the client) handles the domain fronting concerns:
* It makes a connection to https://frontdomain.com but sets the Host header to fronted.site.
* It smuggles the destination Host as the `X-Host` headers.
* It smuggles the desired HTTP method as the `X-Method` header, since the CONNECT method might be disallowed.

The second layer resolves the `X-*` headers above to recover the original request from the browser,
then attempts to function as a normal proxy:
CONNECT requests work as expected, as do other methods (to allow HTTP-only proxying.)

I'm writing this for learning purposes, please don't look here for something reliable at the moment.
