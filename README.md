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
  file named `out.log`

  Note: If you don't specify the cabinet URL on the command line, the tool
  currently tries to read a log file from a hard-coded path. If libkq is
  placed alongside the tool, this will probably work, but please don't rely
  on it.

- Tracks the state of the current game. Much of the state is output into a file
  named `out.csv`, with a row for every state change.

- Runs models to determine which team is winning. The output of one of these
  models is printed to the command line. Additionally, the models can be
  displayed on a [meter](http://localhost:8080/?type=meter) or
  [line graph](http://localhost:8080/) via a web browser.

Other functionality is available from
[kqstats](https://github.com/ughoavgfhw/kqstats). I intend to migrate much of
that functionality here, eventually.
