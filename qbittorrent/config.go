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

func (q *Qbit) ChangePort(ctx context.Context, port uint16) error {
	err := q.ChangeConfig(ctx, map[string]any{
		"listen_port": port,
	})
	if err != nil {
		return fmt.Errorf("ChangePort: %w", err)
	}
	return nil
}

func (q *Qbit) ChangeConfig(ctx context.Context, config map[string]any) error {
	v := url.Values{}
	v.Set("json", string(lo.Must(json.Marshal(config))))
	reqs, err := http.NewRequestWithContext(ctx, "POST", lo.Must(url.JoinPath(q.root, "/api/v2/app/setPreferences")), strings.NewReader(v.Encode()))
	if err != nil {
		return fmt.Errorf("ChangeConfig: %w", err)
	}
	reqs.Header.Add("content-type", "application/x-www-form-urlencoded; charset=UTF-8")
	rep, err := q.c.Do(reqs)
	if err != nil {
		return fmt.Errorf("ChangeConfig: %w", err)
	}
	defer rep.Body.Close()
	if rep.StatusCode != 200 {
		return fmt.Errorf("ChangeConfig: %w", ErrStatusNotOk(rep.StatusCode))
	}
	return nil
}
