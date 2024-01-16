package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
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
	for {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		dosome(ctx, q, banPeerIdReg, banMap)
		cancel()
		time.Sleep(10 * time.Second)
	}
}

func dosome(ctx context.Context, q *qbittorrent.Qbit, banPeerIdReg *regexp.Regexp, needBanMap map[string]time.Time) {
	t, err := q.GetAllTorrents(ctx)
	if err != nil {
		log.Println(err)
		return
	}

	needBanMapL := sync.Mutex{}
	expiredTime := time.Now().Add(2 * time.Hour)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(5)
	for _, item := range t {
		item := item
		if strings.HasPrefix(item.State, "paused") {
			continue
		}
		g.Go(func() error {
			p, err := q.GetPeers(ctx, item.Hash)
			if err != nil {
				return err
			}
			for _, v := range p {
				if banPeerIdReg.MatchString(v.PeerIdClient) {
					needBanMapL.Lock()
					needBanMap[v.IP] = expiredTime
					needBanMapL.Unlock()
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
			continue
		}
		ips = append(ips, k)
	}

	err = q.BanIps(ctx, ips)
	if err != nil {
		log.Println(err)
		return
	}
}
