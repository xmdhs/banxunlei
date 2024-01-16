package qbittorrent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/samber/lo"
)

func getSome[T any](ctx context.Context, path string, c http.Client) (T, error) {
	var t T
	reqs, err := http.NewRequestWithContext(ctx, "GET", path, nil)
	if err != nil {
		return t, err
	}
	rep, err := c.Do(reqs)
	if err != nil {
		return t, err
	}
	defer rep.Body.Close()
	if rep.StatusCode != 200 {
		return t, ErrStatusNotOk(rep.StatusCode)
	}
	jr := json.NewDecoder(rep.Body)
	err = jr.Decode(&t)
	if err != nil {
		return t, err
	}
	return t, nil

}

type TorrentsInfo struct {
	Hash  string `json:"hash"`
	State string `json:"state"`
}

func (q *Qbit) GetAllTorrents(ctx context.Context) ([]TorrentsInfo, error) {
	t, err := getSome[[]TorrentsInfo](ctx, lo.Must(url.JoinPath(q.root, "/api/v2/torrents/info")), q.c)
	if err != nil {
		return nil, fmt.Errorf("GetAllTorrents: %w", err)
	}
	return t, nil
}

type Peer struct {
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	PeerIdClient string `json:"peer_id_client"`
	Client       string `json:"client"`
}

type torrentPeers struct {
	Peers map[string]Peer `json:"peers"`
}

func (q *Qbit) GetPeers(ctx context.Context, hash string) ([]Peer, error) {
	u := lo.Must(url.Parse(lo.Must(url.JoinPath(q.root, "/api/v2/sync/torrentPeers"))))
	v := url.Values{}
	v.Set("hash", hash)
	u.RawQuery = v.Encode()

	t, err := getSome[torrentPeers](ctx, u.String(), q.c)
	if err != nil {
		return nil, fmt.Errorf("GetAllTorrents: %w", err)
	}
	return lo.Values(t.Peers), nil
}

func (q *Qbit) BanIps(ctx context.Context, ip []string) error {
	p, err := getSome[map[string]any](ctx, lo.Must(url.JoinPath(q.root, "/api/v2/app/preferences")), q.c)
	if err != nil {
		return fmt.Errorf("BanIps: %w", err)
	}

	p["banned_IPs"] = strings.Join(ip, "\n")

	v := url.Values{}
	v.Set("json", string(lo.Must(json.Marshal(p))))

	reqs, err := http.NewRequestWithContext(ctx, "POST", lo.Must(url.JoinPath(q.root, "/api/v2/app/setPreferences")), strings.NewReader(v.Encode()))
	if err != nil {
		return fmt.Errorf("BanIps: %w", err)
	}
	reqs.Header.Add("content-type", "application/x-www-form-urlencoded; charset=UTF-8")
	rep, err := q.c.Do(reqs)
	if err != nil {
		return fmt.Errorf("BanIps: %w", err)
	}
	defer rep.Body.Close()
	if rep.StatusCode != 200 {
		return fmt.Errorf("BanIps: %w", ErrStatusNotOk(rep.StatusCode))
	}
	return nil
}
