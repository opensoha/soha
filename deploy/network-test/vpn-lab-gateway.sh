#!/bin/sh
set -eu
# These addresses live on resource loopbacks, outside Docker's host-routed subnet.
/sbin/ip route replace 10.252.250.10/32 via 10.252.240.10
/sbin/ip route replace 10.252.250.11/32 via 10.252.240.11
exec /lab-gateway "$@"
