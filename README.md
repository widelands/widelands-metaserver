# widelands_metaserver

The game server that provides chat and hosting of games for Widelands.

For information about the used network protocol, see the file
[src/network/internet_gaming_protocol.h](http://bazaar.launchpad.net/~widelands-dev/widelands/trunk/view/head:/src/network/internet_gaming_protocol.h)
in the Widelands sources at <https://launchpad.net/widelands>.

# Building

1. Install [Go](https://golang.org/doc/install).
2. `cd $GOPATH`
3. `go get github.com/widelands/widelands_metaserver/wlms`
4. `cd src/github.com/widelands/widelands_metaserver/wlms`
5. `make`

# Deploying

1. `make cross`
2. scp `wlms` over to the server and replace `/usr/local/bin/wlms`.
3. `sudo restart wlmetaserver`
4. Check in `/var/log/upstart/wlmetaserver.log` that the restart was
   successful.
