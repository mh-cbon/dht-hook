# dht-hook

`dht-hook` pushes `dht` announces to `http` remotes.

The cli runs
- a dht node that people can use to announce their torrents to.
- an http server to receive hook subscription.

It listens to new announce info hashes received on the dht node
and pushes them at interval to remotes subscribed.

# install

```sh
go get github.com/mh-cbon/dht-hook
```

# cli

```sh
$ dha -help
Usage:
  dha [OPTIONS...]
Options:
  -allowSub                 (bool)            enable subscribe route
  -announceMaxTimeout       (time.Duration)   max timeout before an announce is evicted (Default: 240h0m0s)
  -debug                    (bool)            enable debug routes (Default: true)
  -httPlisten               (string)          local tcp address to receive subscriptions (Default: :7945)
  -listen                   (string)          local UDP address of the dht node (Default: :0)
  -maxAnnounces             (int)             maximum number of announces to keep track (Default: 10000000)
  -maxFailures              (int)             number of failures before a remote is put on hold (Default: 5)
  -maxRemotes               (int)             maximum number of remotes (Default: 1)
  -remote                   (string)          remote address to send announce notifications to
  -remoteOnHoldMaxTimeout   (time.Duration)   max timeout before eviction of a remote put on hold (Default: 72h0m0s)
  -tableFile                (string)          name of file for storing node info (Default: bootstrap.compact)
```
