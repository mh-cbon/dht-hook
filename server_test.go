package hook

import (
	"encoding/json"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

func TestServer(t *testing.T) {
	t.Run("1", func(t *testing.T) {
		wait := make(chan bool)
		h := http.Server{
			Addr: "127.0.0.1:8080",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/notify" {
					log.Println("notified")
					res := []metainfo.Hash{}
					err := json.NewDecoder(r.Body).Decode(&res)
					if err != nil {
						t.Fatal(err)
					}
					log.Println(res)
					if len(res) != 2 {
						t.Fatal("wanted 2 hash got", len(res))
					}
					wait <- true
				}
			}),
		}
		go func() {
			if err := h.ListenAndServe(); err != nil {
				t.Fatalf("http listen %v", err)
			}
		}()
		s := New(&Config{
			AnnounceMaxTimeout:     time.Second,
			MaxAnnounces:           2,
			MaxFailures:            1,
			MaxRemotes:             1,
			MinRemoteInterval:      time.Second * 2,
			RemoteOnHoldMaxTimeout: time.Second,
		})
		if err := s.Subscribe("http://127.0.0.1:8080/notify", time.Second*3); err != nil {
			t.Fatal("subscribe hook", err)
		}
		if err := s.Subscribe("http://127.0.0.1:8081/notify", time.Second*3); err == nil {
			t.Fatal("must error exceeded max remote count")
		}
		if err := s.Subscribe("http://127.0.0.1:8080/notify", time.Millisecond); err == nil {
			t.Fatal("must error notify interval is too short")
		}
		time.After(time.Second)
		infoHash := metainfo.NewHashFromHex("52fdfc072182654f163f5f0f9a621d729566c74d")
		if err := s.Announce(infoHash); err != nil {
			t.Fatalf("info hash announce %v", err)
		}
		time.After(time.Second)
		infoHash = metainfo.NewHashFromHex("02fdfc072182654f163f5f0f9a621d729566c74d")
		if err := s.Announce(infoHash); err != nil {
			t.Fatalf("info hash announce %v", err)
		}
		time.After(time.Second)
		infoHash = metainfo.NewHashFromHex("12fdfc072182654f163f5f0f9a621d729566c74d")
		if err := s.Announce(infoHash); err == nil {
			t.Fatal("must error exceeded max announces count")
		}
		log.Println("waiting...")
		<-wait
	})
}
