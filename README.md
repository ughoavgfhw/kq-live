# kq-live
A utility for use with Killer Queen Arcade. Get live statistics, power streaming overlays, and more.

## Basic Usage

First, connect your computer to the same network as the killer queen cabinet.
Then:

```sh
./kq-live ws://kq.local:12749
```

Note: Resolving the `kq.local` host name requires that your system is running
multicast DNS, and that the mDNS packets are not blocked by the network
configuration. mDNS is typically available by default on macOS and linux
systems, while Windows systems may or may not have it. It is always possible to
use the cab's IP address instead of the host name.

## Installation

1. Install go from https://golang.org/dl/
1. Download the source code and dependencies.
   ```sh
   go get github.com/ughoavgfhw/kq-live
   ```
1. Generate the embedded assets.
   ```sh
   go generate github.com/ughoavgfhw/kq-live/assets
   ```
   This must be done whenever any non-go files in the assets directory change,
   as the binary embeds the file as of the previous generate.

   Note: If you are actively modifying assets, the binary can be built with
   `-tags=dev` to read assets from disk instead of embedding them. However,
   the dev build assumes that the assets directory can be found at ./assets.
1. Build the tool. There are two options here.
   - Build and install into the go binary directory, which can be added to your
     shell path.
     ```sh
     go install github.com/ughoavgfhw/kq-live
     ```
   - Just build the tool, leaving the binary inside the current directory.
     ```sh
     go build github.com/ughoavgfhw/kq-live
     ```

## Existing Functionality

- Reads events from the killer queen cabinet. All messages are output into a
  file named `out.log`.

  Note: If you don't specify the cabinet URL on the command line, the tool
  will use the default `ws://kq.local:12749`.

- Tracks the state of the current game. Much of the state is output into a file
  named `out.csv`, with a row for every state change.

- Runs models to determine which team is winning. The output of one of these
  models is printed to the command line. Additionally, the models can be
  displayed on a [meter](http://localhost:8080/?type=meter) or
  [line graph](http://localhost:8080/) via a web browser.

- Provides various web pages useful for streaming overlays.
  - [Scoreboard](http://localhost:8080/scoreboard) and a
    [control interface](http://localhost:8080/control/scores).
  - Basic statistics for [blue](http://localhost:8080/statsboard/blue) and
    [gold](http://localhost:8080/statsboard/gold) teams. There is also a larger
    [statistics chart](http://localhost:8080/stats).
  - Indicator of [famine state](http://localhost:8080/famineTracker).
  - [Player photos](http://localhost:8080/teamPictures) for the current teams.
