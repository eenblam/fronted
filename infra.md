Bought on namecheap

ns1.digitalocean.com.
ns2.digitalocean.com.
ns3.digitalocean.com.

Digital Ocean:

* Created project and IP via UI. `backend.fronted.site` has A record to IP.
* Create droplet, associate with project, add fixed IP via `deploy.sh`
* TODO:
    * need LE for backend!
        * Could just use a DO load balancer, but even for 1 node it's $12/mo!
    * need Fastly iptables for backend!

Fastly:

* Create service `fronted.site`, add domain of same name
* Add origin host `backend.fronted.site`
* Set up certificate for fronted.site (Under https://manage.fastly.com/secure)
    * Takes you to https://manage.fastly.com/network/subscriptions/new
* Get anycast details for certificate
    * Visit https://manage.fastly.com/network/domains
    * Find your cert and click View Details on the right
    * Find the list of A records from the dropdown
    * Add each distinct A record to your DigitalOcean NS for the domain (you have to use `@` for the apex)

So my Digital Ocean DNS now looks like this (within my fronted.site project):

```
Type   Hostname                      Value                                      TTL (seconds) 	
A      fronted.site                  151.101.193.91                             3600
A      fronted.site                  151.101.129.91                             3600
A      fronted.site                  151.101.65.91                              3600
A      fronted.site                  151.101.1.91                               3600
CNAME  _acme-challenge.fronted.site  12dkugjg3k0ockogb1.fastly-validations.com  43200
A      backend.fronted.site          164.90.245.155                             3600
NS     fronted.site                  ns1.digitalocean.com.                      1800
NS     fronted.site                  ns2.digitalocean.com.                      1800
NS     fronted.site                  ns3.digitalocean.com.                      1800
```

Cert at `/etc/letsencrypt/live/backend.fronted.site/fullchain.pem`
Key lives at `/etc/letsencrypt/live/backend.fronted.site/privkey.pem`
