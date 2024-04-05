package main

import (
	"context"
	"encoding/json"
	"errors"
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

	var q *qbittorrent.Qbit
	_, _, err := lo.AttemptWithDelay(5, 1*time.Second, func(index int, duration time.Duration) error {
		var err error
		q, err = qbittorrent.Login(ctx, c.Root, http.Client{Timeout: 10 * time.Second}, c.UserName, c.PassWord)
		return err
	})
	lo.Must0(err)

	banMap := map[string]time.Time{}

	banPeerIdReg := regexp.MustCompile(c.BanPeerIdReg)
	banClientReg := regexp.MustCompile(c.BanClientReg)
	for {
		func() {
			sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			defer time.Sleep(10 * time.Second)
			err := scan(sctx, q, banPeerIdReg, banClientReg, banMap)
			if err != nil {
				log.Println(err)
				var ec qbittorrent.ErrStatusNotOk
				if errors.As(err, &ec) && int(ec) == 403 {
					log.Println("重新登录")
					q, err = qbittorrent.Login(ctx, c.Root, http.Client{Timeout: 10 * time.Second}, c.UserName, c.PassWord)
					if err != nil {
						log.Println(err)
					}
				}
				return
			}

			if len(banMap) == 0 {
				banMap = map[string]time.Time{}
			}
		}()
	}
}

func scan(ctx context.Context, q *qbittorrent.Qbit, banPeerIdReg, banClientReg *regexp.Regexp, needBanMap map[string]time.Time) error {
	t, err := q.GetAllTorrents(ctx)
	if err != nil {
		return err
	}

	needBanMapL := sync.Mutex{}
	expiredTime := time.Now().Add(12 * time.Hour)
	needChange := atomic.Bool{}

	peerIdCheck := peerIdCheck(banPeerIdReg, banClientReg)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5)
	for _, item := range t {
		if item.UpSpeed == 0 {
			continue
		}
		item := item
		progressCheck := progressCheck(item.TotalSize)
		g.Go(func() error {
			p, err := q.GetPeers(gctx, item.Hash)
			if err != nil {
				return err
			}
			setBanIp := func(s string) {
				needBanMapL.Lock()
				needBanMap[s] = expiredTime
				needBanMapL.Unlock()
				needChange.Store(true)
			}
			m := map[string]check{
				"客户端规则命中": peerIdCheck,
				"进度规则命中":  progressCheck,
			}
			for _, v := range p {
				for k, check := range m {
					if check(v) {
						setBanIp(v.IP)
						log.Printf("ip: %v peerID: %v client: %v name: %v reason: %v uploaded: %v progress: %v totalSize: %v", v.IP, v.PeerIdClient,
							v.Client, item.Name, k, v.Uploaded, v.Progress, item.TotalSize)
						break
					}
				}
			}
			return nil
		})

	}

	err = g.Wait()
	if err != nil {
		return err
	}

	ips := []string{}
	now := time.Now().Truncate(time.Hour)
	for k, v := range needBanMap {
		if now.After(v) {
			delete(needBanMap, k)
			needChange.Store(true)
			continue
		}
		ips = append(ips, k)
	}

	if !needChange.Load() {
		return nil
	}

	err = q.BanIps(ctx, ips)
	if err != nil {
		return err
	}
	return nil
}
