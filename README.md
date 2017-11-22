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
2. `scp wlms ssh://widelands.org:`. Then ssh into the machine and overwrite `/usr/local/bin/wlms` with the new binary.
3. `sudo restart wlmetaserver`
4. Check in `/var/log/upstart/wlmetaserver.log` that the restart was
   successful.

# Testing locally

1. `./wlms`. This starts the server with an empty in memory user database.
2. Edit `~/.widelands/config` and add the line `metaserver="127.0.0.1"` before
   launching widelands.
3. Launch Widelands and click on internet game.
4. Do not forget to remove the metaserver line once you want to play on the real
   metaserver again.
