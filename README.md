# widelands-metaserver

The game server that provides chat and hosting of games for Widelands.

For information about the used network protocol, see the file
[src/network/internet_gaming_protocol.h](https://github.com/widelands/widelands/blob/master/src/network/internet_gaming_protocol.h)
in the Widelands sources at <https://github.com/widelands/widelands>.

# Building

1. Install [Go](https://go.dev/doc/install) 1.22 or newer.
2. Clone this repository anywhere; the dependencies are pinned in `go.mod` and `go.sum`.
3. `make`. This builds `bin/wlms` (metaserver) and `bin/wlnr` (relay).

# Deploying

1. `make cross`. This builds static Linux binaries in `bin/linux_amd64/`.
2. scp `bin/linux_amd64/wl*` over to the server and replace the files in `/usr/local/bin/`
   (keep the old binaries for a rollback).
3. `sudo systemctl restart wl-netrelay wl-metaserver`
4. Check with `journalctl -u wl-netrelay -u wl-metaserver` that the restarts were successful.

# Testing locally

1. `bin/wlnr`. This starts the relay server for hosting games.
2. `bin/wlms`. This starts the server with an empty in memory user database.
3. Edit `~/.widelands/config` and add the line `metaserver="localhost"` before
   launching widelands.
4. Launch Widelands and click on internet game.
5. Do not forget to remove the metaserver line once you want to play on the real
   metaserver again.
