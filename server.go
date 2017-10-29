//Package hook provides a server to notify remotes of dht announces.
package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

//Server of announces notifications
type Server struct {
	announces []*announce
	remotes   []*remote

	annLock    sync.RWMutex
	remoteLock sync.RWMutex

	config Config
}

//Config settings
type Config struct {
	RemoteOnHoldMaxTimeout time.Duration
	MaxFailures            int
	MaxRemotes             int
	MinRemoteInterval      time.Duration
	AnnounceMaxTimeout     time.Duration
	MaxAnnounces           int
}

type remote struct {
	addr     string
	lastCall *time.Time
	interval time.Duration
	failures int
	onHold   *time.Time
}

type announce struct {
	infoHash     metainfo.Hash
	lastAnnounce time.Time
}

//DefaultConfig for the server
var DefaultConfig = &Config{
	RemoteOnHoldMaxTimeout: time.Hour * 24 * 3,
	MaxFailures:            5,
	MaxRemotes:             1,
	AnnounceMaxTimeout:     time.Hour * 24 * 10,
	MinRemoteInterval:      time.Hour,
	MaxAnnounces:           10000000, //?
}

//New server notifier of dht announces
func New(c *Config) *Server {
	if c == nil {
		c = DefaultConfig
	}
	ret := &Server{
		announces:  []*announce{},
		remotes:    []*remote{},
		annLock:    sync.RWMutex{},
		remoteLock: sync.RWMutex{},
		config:     *c,
	}
	go ret.start()
	return ret
}

func (s *Server) start() {
	for {
		<-time.After(time.Second)
		s.remoteLock.Lock()
		for _, r := range s.remotes {
			if r.onHold != nil {
				continue
			}
			ref := time.Now().Add(-r.interval)
			if r.lastCall != nil {
				ref = *r.lastCall
				ref = ref.Add(r.interval)
			}
			if r.lastCall == nil || ref.Before(time.Now()) {
				announces := s.collectAnnounces(ref)
				if len(announces) > 0 {
					if err := s.send(r, announces); err != nil {
						log.Println(err)
						r.failures++
						if r.failures > s.config.MaxFailures {
							t := time.Now()
							r.onHold = &t
						}
						continue
					}
					r.failures = 0
					p := time.Now()
					r.lastCall = &p
				}
			}
		}
		for i := len(s.remotes) - 1; i >= 0; i-- {
			r := s.remotes[i]
			if r.onHold != nil && r.onHold.Add(s.config.RemoteOnHoldMaxTimeout).After(time.Now()) {
				s.remotes = append(s.remotes[:i], s.remotes[i+1:]...)
			}
		}
		s.remoteLock.Unlock()

		s.annLock.Lock()
		for i := len(s.announces) - 1; i >= 0; i-- {
			a := s.announces[i]
			if a.lastAnnounce.Add(s.config.AnnounceMaxTimeout).Before(time.Now()) {
				s.announces = append(s.announces[:i], s.announces[i+1:]...)
			}
		}
		s.annLock.Unlock()
	}
}

func (s *Server) send(r *remote, data []metainfo.Hash) error {
	body := new(bytes.Buffer)
	err := json.NewEncoder(body).Encode(data)
	if err != nil {
		return err
	}
	res, err := http.Post(r.addr, "application/json", body)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		return errors.New("notok")
	}
	return nil
}

func (s *Server) collectAnnounces(since time.Time) []metainfo.Hash {
	s.annLock.Lock()
	defer s.annLock.Unlock()
	ret := []metainfo.Hash{}
	for _, a := range s.announces {
		if a.lastAnnounce.After(since) {
			ret = append(ret, a.infoHash)
		}
	}
	return ret
}

func (s *Server) hasAnnounce(infoHash metainfo.Hash) bool {
	for _, r := range s.announces {
		if r.infoHash == infoHash {
			return true
		}
	}
	return false
}
func (s *Server) getAnnounce(infoHash metainfo.Hash) *announce {
	for _, r := range s.announces {
		if r.infoHash == infoHash {
			return r
		}
	}
	return nil
}

//Announce a info hash.
func (s *Server) Announce(infoHash metainfo.Hash) error {
	announcesRcv.Add(1)
	s.annLock.Lock()
	defer s.annLock.Unlock()
	if a := s.getAnnounce(infoHash); a != nil {
		a.lastAnnounce = time.Now()
		return nil
	}
	if len(s.announces)+1 > s.config.MaxAnnounces {
		return errors.New("try again later")
	}
	s.announces = append(s.announces, &announce{
		infoHash:     infoHash,
		lastAnnounce: time.Now(),
	})
	return nil
}

func (s *Server) hasRemote(remote string) bool {
	for _, r := range s.remotes {
		if r.addr == remote {
			return true
		}
	}
	return false
}

func (s *Server) getRemote(remote string) *remote {
	for _, r := range s.remotes {
		if r.addr == remote {
			return r
		}
	}
	return nil
}

//Subscribe a remote to notify announces.
func (s *Server) Subscribe(uri string, interval time.Duration) error {
	s.remoteLock.Lock()
	defer s.remoteLock.Unlock()
	_, err := url.Parse(uri)
	if err != nil {
		return err
	}
	if interval < s.config.MinRemoteInterval {
		return errors.New("interval is too short")
	}
	if r := s.getRemote(uri); r != nil {
		r.interval = interval
		r.onHold = nil
		r.failures = 0
		return nil
	}
	if len(s.remotes)+1 > s.config.MaxRemotes {
		return errors.New("try again later")
	}
	s.remotes = append(s.remotes, &remote{
		addr:     uri,
		interval: interval,
	})
	return nil
}

//WriteStatus prints some stats.
func (s *Server) WriteStatus(w io.Writer) error {
	var remotes int
	var announces int
	s.remoteLock.Lock()
	remotes = len(s.remotes)
	s.remoteLock.Unlock()
	s.annLock.Lock()
	announces = len(s.announces)
	s.annLock.Unlock()
	fmt.Fprintf(w, "remotes %v\n", remotes)
	fmt.Fprintf(w, "announces %v\n", announces)
	return nil
}
