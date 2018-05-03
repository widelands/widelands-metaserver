# widelands_metaserver

The game server that provides chat and hosting of games for Widelands.

For information about the used network protocol, see the file
[src/network/internet_gaming_protocol.h](http://bazaar.launchpad.net/~widelands-dev/widelands/trunk/view/head:/src/network/internet_gaming_protocol.h)
in the Widelands sources at <https://launchpad.net/widelands>.

# Building

1. Install [Go](https://golang.org/doc/install).
2. `cd $GOPATH`
3. `go get github.com/widelands/widelands_metaserver/...`
4. `cd src/github.com/widelands/widelands_metaserver`
5. `make`

# Deploying

1. `make cross`
2. scp `$GOPATH/bin/linux_amd64/wl*` over to the server and replace the files in `/usr/local/bin/`.
3. `sudo restart wlnetrelay`. This will also restart the metaserver.
4. Check in `/var/log/upstart/wlmetaserver.log` and
   `/var/log/upstart/wlnetrelay.log` that the restarts were
   successful.

# Testing locally

1. `$GOPATH/bin/wlnr`. This starts the relay server for hosting games.
2. `$GOPATH/bin/wlms`. This starts the server with an empty in memory user database.
3. Edit `~/.widelands/config` and add the line `metaserver="localhost"` before
   launching widelands.
4. Launch Widelands and click on internet game.
5. Do not forget to remove the metaserver line once you want to play on the real
   metaserver again.
