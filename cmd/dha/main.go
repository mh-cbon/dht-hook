//Package dha push dht announces to http remotes.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/anacrolix/dht"
	"github.com/anacrolix/dht/krpc"
	"github.com/anacrolix/tagflag"
	"github.com/anacrolix/torrent/metainfo"
	hook "github.com/mh-cbon/dht-hook"
)

var (
	flags = struct {
		TableFile      string        `help:"name of file for storing node info"`
		Listen         string        `help:"local UDP address of the dht node"`
		Remote         string        `help:"remote address to send announce notifications to"`
		RemoteInterval time.Duration `help:"remote push interval"`
		HTTPlisten     string        `help:"local tcp address to receive subscriptions"`
		Debug          bool          `help:"enable debug routes"`
		AllowSub       bool          `help:"enable subscribe route"`

		RemoteOnHoldMaxTimeout time.Duration `help:"max timeout before eviction of a remote put on hold"`
		MaxFailures            int           `help:"number of failures before a remote is put on hold"`
		MaxRemotes             int           `help:"maximum number of remotes"`
		AnnounceMaxTimeout     time.Duration `help:"max timeout before an announce is evicted"`
		MaxAnnounces           int           `help:"maximum number of announces to keep track"`
	}{
		Listen:                 ":0",
		HTTPlisten:             ":7945",
		TableFile:              "bootstrap.compact",
		RemoteInterval:         time.Minute * 10,
		Debug:                  true,
		AllowSub:               false,
		RemoteOnHoldMaxTimeout: hook.DefaultConfig.RemoteOnHoldMaxTimeout,
		MaxFailures:            hook.DefaultConfig.MaxFailures,
		MaxRemotes:             hook.DefaultConfig.MaxRemotes,
		AnnounceMaxTimeout:     hook.DefaultConfig.AnnounceMaxTimeout,
		MaxAnnounces:           hook.DefaultConfig.MaxAnnounces,
	}
	s *dht.Server
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	tagflag.Parse(&flags)

	//setup the hook server
	var hookServer *hook.Server
	{
		hookServer = hook.New(&hook.Config{
			RemoteOnHoldMaxTimeout: flags.RemoteOnHoldMaxTimeout,
			MaxFailures:            flags.MaxFailures,
			MaxRemotes:             flags.MaxRemotes,
			AnnounceMaxTimeout:     flags.AnnounceMaxTimeout,
			MaxAnnounces:           flags.MaxAnnounces,
		})
		if flags.Remote != "" {
			if err := hookServer.Subscribe(flags.Remote, flags.RemoteInterval); err != nil {
				panic(err)
			}
		}
	}

	//setup the dht node
	{
		conn, err := net.ListenPacket("udp", flags.Listen)
		if err != nil {
			log.Fatal(err)
		}
		defer conn.Close()
		s, err = dht.NewServer(&dht.ServerConfig{
			Conn:          conn,
			StartingNodes: dht.GlobalBootstrapAddrs,
			OnAnnouncePeer: func(infoHash metainfo.Hash, peer dht.Peer) {
				if errAnnounce := hookServer.Announce(infoHash); errAnnounce != nil {
					log.Println(errAnnounce)
				}
			},
		})
		if err != nil {
			log.Fatal(err)
		}
		err = loadTable()
		if err != nil {
			log.Fatalf("error loading table: %s", err)
		}
	}

	//setup hooks handler
	{
		if flags.AllowSub {
			server := http.Server{
				Addr: flags.HTTPlisten,
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/sub" {
						remote := r.URL.Query().Get("remote")
						remote = strings.TrimSpace(remote)
						interval, err := time.ParseDuration(r.URL.Query().Get("interval"))
						if len(remote) == 0 {
							http.Error(w, "remote argument is required", http.StatusBadRequest)
							return
						}
						if err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						if err := hookServer.Subscribe(remote, interval); err != nil {
							http.Error(w, "remote argument is required", http.StatusBadRequest)
							return
						}
					} else if r.URL.Path == "/debug/hooks" {
						if flags.Debug {
							hookServer.WriteStatus(w)
						}
					} else if r.URL.Path == "/debug/dht" {
						if flags.Debug {
							s.WriteStatus(w)
						}
					}
				}),
			}
			go func() {
				if err := server.ListenAndServe(); err != nil {
					panic(err)
				}
			}()
		}
	}

	log.Printf("dht server on %s, ID is %x", s.Addr(), s.ID())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ch := make(chan os.Signal)
		signal.Notify(ch)
		<-ch
		cancel()
	}()

	go func() {
		if tried, err := s.Bootstrap(); err != nil {
			log.Printf("error bootstrapping: %s", err)
		} else {
			log.Printf("finished bootstrapping: crawled %d addrs", tried)
		}
	}()

	go func() {
		if tried, err := s.Bootstrap(); err != nil {
			log.Printf("error bootstrapping: %s", err)
		} else {
			log.Printf("finished bootstrapping: crawled %d addrs", tried)
		}
	}()
	<-ctx.Done()
	s.Close()

	// saves bootstrap
	if flags.TableFile != "" {
		if err := saveTable(); err != nil {
			log.Printf("error saving node table: %s", err)
		}
	}
}

func loadTable() error {
	if flags.TableFile == "" {
		return nil
	}
	f, err := os.Open(flags.TableFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error opening table file: %s", err)
	}
	defer f.Close()
	added := 0
	for {
		b := make([]byte, krpc.CompactIPv4NodeInfoLen)
		_, err := io.ReadFull(f, b)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading table file: %s", err)
		}
		var ni krpc.NodeInfo
		err = ni.UnmarshalCompactIPv4(b)
		if err != nil {
			return fmt.Errorf("error unmarshaling compact node info: %s", err)
		}
		s.AddNode(ni)
		added++
	}
	log.Printf("loaded %d nodes from table file", added)
	return nil
}

func saveTable() error {
	goodNodes := s.Nodes()
	if flags.TableFile == "" {
		if len(goodNodes) != 0 {
			log.Printf("discarding %d good nodes!", len(goodNodes))
		}
		return nil
	}
	f, err := os.OpenFile(flags.TableFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		return fmt.Errorf("error opening table file: %s", err)
	}
	defer f.Close()
	for _, nodeInfo := range goodNodes {
		var b [krpc.CompactIPv4NodeInfoLen]byte
		err := nodeInfo.PutCompact(b[:])
		if err != nil {
			return fmt.Errorf("error compacting node info: %s", err)
		}
		_, err = f.Write(b[:])
		if err != nil {
			return fmt.Errorf("error writing compact node info: %s", err)
		}
	}
	log.Printf("saved %d nodes to table file", len(goodNodes))
	return nil
}
