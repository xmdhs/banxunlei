package qbittorrent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"github.com/samber/lo"
)

type Qbit struct {
	root string
	c    http.Client
}

type ErrStatusNotOk int

func (e ErrStatusNotOk) Error() string {
	return fmt.Sprintf("status %d", int(e))
}

func Login(ctx context.Context, root string, c http.Client, username, password string) (*Qbit, error) {
	c.Jar = lo.Must(cookiejar.New(nil))

	q := Qbit{
		c:    c,
		root: root,
	}

	v := url.Values{}
	v.Set("username", username)
	v.Set("password", password)

	reqs, err := http.NewRequestWithContext(ctx, "POST", lo.Must(url.JoinPath(q.root, "/api/v2/auth/login")), strings.NewReader(v.Encode()))
	if err != nil {
		return nil, fmt.Errorf("Login: %w", err)
	}
	rep, err := c.Do(reqs)
	if err != nil {
		return nil, fmt.Errorf("Login: %w", err)
	}
	defer rep.Body.Close()

	if rep.StatusCode != 200 {
		return nil, fmt.Errorf("Login: %w", ErrStatusNotOk(rep.StatusCode))
	}
	return &q, nil
}
