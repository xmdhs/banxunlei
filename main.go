package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samber/lo"
	"github.com/xmdhs/banxunlei/qbittorrent"
	"golang.org/x/sync/errgroup"
)

var configPath string

func init() {
	flag.StringVar(&configPath, "c", "config.json", "")
	flag.Parse()
}

func main() {
	c := config{}
	b := lo.Must(os.ReadFile(configPath))
	lo.Must0(json.Unmarshal(b, &c))

	ctx := context.Background()

	q := lo.Must(qbittorrent.Login(ctx, c.Root, http.Client{Timeout: 10 * time.Second}, c.UserName, c.PassWord))

	banMap := map[string]time.Time{}

	banPeerIdReg := regexp.MustCompile(c.BanPeerIdReg)
	banClientReg := regexp.MustCompile(c.BanClientReg)
	for {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		dosome(ctx, q, banPeerIdReg, banClientReg, banMap)
		cancel()
		time.Sleep(10 * time.Second)
	}
}

func dosome(ctx context.Context, q *qbittorrent.Qbit, banPeerIdReg, banClientReg *regexp.Regexp, needBanMap map[string]time.Time) {
	t, err := q.GetAllTorrents(ctx)
	if err != nil {
		log.Println(err)
		return
	}

	needBanMapL := sync.Mutex{}
	expiredTime := time.Now().Add(2 * time.Hour)
	needChange := atomic.Bool{}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5)
	for _, item := range t {
		if item.UpSpeed == 0 {
			continue
		}
		item := item
		g.Go(func() error {
			p, err := q.GetPeers(gctx, item.Hash)
			if err != nil {
				return err
			}
			for _, v := range p {
				if banPeerIdReg.MatchString(v.PeerIdClient) || banClientReg.MatchString(v.Client) {
					needBanMapL.Lock()
					needBanMap[v.IP] = expiredTime
					needBanMapL.Unlock()
					log.Println(v.IP, v.PeerIdClient, v.Client, item.Name)
					needChange.Store(true)
				}
			}
			return nil
		})

	}

	err = g.Wait()
	if err != nil {
		log.Println(err)
		return
	}

	ips := []string{}
	now := time.Now()
	for k, v := range needBanMap {
		if now.After(v) {
			delete(needBanMap, k)
			needChange.Store(true)
			continue
		}
		ips = append(ips, k)
	}

	if !needChange.Load() {
		return
	}

	err = q.BanIps(ctx, ips)
	if err != nil {
		log.Println(err)
		return
	}
}
