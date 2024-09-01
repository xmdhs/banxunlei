package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"net/netip"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/go-units"
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
	banIpCidMap := map[netip.Prefix]time.Time{}

	banPeerIdReg := regexp.MustCompile(c.BanPeerIdReg)
	banClientReg := regexp.MustCompile(c.BanClientReg)
	ban := ban{
		banPeerIdReg:     banPeerIdReg,
		banClientReg:     banClientReg,
		needBanMap:       banMap,
		needBanIpCidrMap: banIpCidMap,
	}
	client := http.Client{Timeout: 10 * time.Second}

	lo.Must0(ban.update(ctx, c.ExternalBanListURL, client))

	go func() {
		t := time.NewTicker(12 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				err := ban.update(ctx, c.ExternalBanListURL, client)
				if err != nil {
					log.Println(err)
					continue
				}
				log.Println("更新外部列表成功")
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		func() {
			sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			defer time.Sleep(10 * time.Second)

			err := ban.scan(sctx, q)
			if err != nil {
				log.Println(err)
				var ec qbittorrent.ErrStatusNotOk
				if errors.As(err, &ec) && int(ec) == 403 {
					log.Println("重新登录")
					q, err = qbittorrent.Login(ctx, c.Root, client, c.UserName, c.PassWord)
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

type ban struct {
	banPeerIdReg, banClientReg *regexp.Regexp
	needBanMap                 map[string]time.Time
	needBanIpCidrMap           map[netip.Prefix]time.Time
	externalBanIpCidr          atomic.Pointer[[]netip.Prefix]
}

func (b *ban) update(ctx context.Context, url string, c http.Client) error {
	_, _, err := lo.AttemptWithDelay(5, 1*time.Second, func(index int, duration time.Duration) error {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return err
		}
		reps, err := c.Do(req)
		if err != nil {
			return err
		}
		defer reps.Body.Close()
		list := []netip.Prefix{}
		s := bufio.NewScanner(reps.Body)
		for s.Scan() {
			t := s.Text()
			p, err := netip.ParsePrefix(t)
			if err != nil {
				p, _ = getPrefix(t)
			}
			if p.IsValid() {
				list = append(list, p)
			}
		}
		b.externalBanIpCidr.Store(&list)
		return nil
	})
	return err
}

func (b *ban) scan(ctx context.Context, q *qbittorrent.Qbit) error {
	t, err := q.GetAllTorrents(ctx)
	if err != nil {
		return err
	}

	needBanMapL := sync.Mutex{}
	expiredTime := time.Now().Add(12 * time.Hour)
	needChange := atomic.Bool{}

	peerIdCheck := peerIdCheck(b.banPeerIdReg, b.banClientReg)
	checkIpCidrFunc := checkIpCidr(b.needBanIpCidrMap)
	externalIpCidr := checkIpCidrList(*b.externalBanIpCidr.Load())

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
			setBanIp := func(s string) error {
				needBanMapL.Lock()
				b.needBanMap[s] = expiredTime
				addr, err := getPrefix(s)
				if err != nil {
					return err
				}
				b.needBanIpCidrMap[addr] = expiredTime
				needBanMapL.Unlock()
				needChange.Store(true)
				return nil
			}
			checkList := []struct {
				name  string
				check check
			}{
				{name: "客户端规则命中", check: peerIdCheck},
				{name: "ip 段黑名单", check: checkIpCidrFunc},
				{name: "外部 ip 段黑名单", check: externalIpCidr},
				{name: "进度规则命中", check: progressCheck},
			}
			for _, v := range p {
				for _, c := range checkList {
					if c.check(v) {
						err = setBanIp(v.IP)
						if err != nil {
							log.Println(err)
						}
						log.Printf("ip: %v peerID: %v client: %v name: %v reason: %v uploaded: %v progress: %v totalSize: %v", v.IP, v.PeerIdClient,
							v.Client, item.Name, c.name, units.HumanSize(float64(v.Uploaded)), v.Progress, units.HumanSize(float64(item.TotalSize)))
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
	for k, v := range b.needBanMap {
		if now.After(v) {
			delete(b.needBanMap, k)
			needChange.Store(true)
			continue
		}
		ips = append(ips, k)
	}
	for k, v := range b.needBanIpCidrMap {
		if now.After(v) {
			delete(b.needBanIpCidrMap, k)
			continue
		}
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
