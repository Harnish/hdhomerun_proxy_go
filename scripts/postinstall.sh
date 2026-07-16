#!/bin/sh
set -e

getent group hdhomerun >/dev/null 2>&1 || groupadd -r hdhomerun
getent passwd hdhomerun >/dev/null 2>&1 || useradd -r -s /sbin/nologin -d /opt/hdhomerun-proxy -g hdhomerun hdhomerun

chown -R hdhomerun:hdhomerun /opt/hdhomerun-proxy

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
fi

exit 0
