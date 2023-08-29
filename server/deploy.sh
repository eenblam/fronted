#!/bin/bash

log() {
echo "$1" >&2
}

#ubuntu-s-1vcpu-1gb-sfo2-01
NAME=fronted.site

DROPLET=$(doctl compute droplet get $NAME --format=ID --no-header 2>/dev/null)
if [ -z "$DROPLET" ]; then
	log "Droplet ${NAME} not found. Creating."
	if DROPLET=$(doctl compute droplet create \
		--image ubuntu-22-10-x64 \
		--size s-1vcpu-1gb \
		--region sfo3 \
		--vpc-uuid f787bd48-166d-4bf2-9708-7429601f8603 \
		--ssh-keys c8:79:0b:65:47:36:b8:77:83:8e:97:cf:c5:3b:90:0b \
		--enable-monitoring \
		--wait \
		"$NAME" \
		--format=ID \
		--no-header)
	then
		log "Created droplet ${NAME} with ID ${DROPLET}."
	else
		log "Couldn't create droplet ${NAME}. Exiting."
		exit 1
        fi
else
	log "Found droplet ${NAME} with ID ${DROPLET}."
fi

PROJECT=55a03a50-9df5-42c4-a5fd-4522a87f4df6
doctl projects resources assign "$PROJECT" --resource="do:droplet:${DROPLET}"

# This is a $6/mo droplet
# Reserved IP is free when assigned, but $5/mo when unassigned.
# So we only save a buck by taking down the droplet :p
# We also need --wait on doctl compute droplet create before we can call this!
IP="164.90.245.155"
doctl compute reserved-ip-action assign "$IP" "$DROPLET"

# There's already an A record for backend.fronted.site to the above IP

KEY=~/.ssh/id_rsa_do
TARGET="proxy"

CGO_ENABLED=0 go build -o "$TARGET" .

scp -i "$KEY" \
	-o StrictHostKeyChecking=accept-new \
	"./${TARGET}" "root@${IP}:/root/"

echo -- ssh -i "$KEY" "root@${IP}"
