package main

import (
	"regexp"

	"github.com/xmdhs/banxunlei/qbittorrent"
)

type check func(qbittorrent.Peer) bool

func peerIdCheck(banPeerIdReg, banClientReg *regexp.Regexp) check {
	return func(p qbittorrent.Peer) bool {
		return banPeerIdReg.MatchString(p.PeerIdClient) || banClientReg.MatchString(p.Client)
	}
}

func progressCheck(totalSize int) check {
	return func(p qbittorrent.Peer) bool {
		if p.Uploaded < max(min(100*1000*1000, int(float64(totalSize)*0.1)), 20*1000*1000) {
			return false
		}
		if p.Uploaded+50*1000*1000 > totalSize {
			return true
		}
		if float64(p.Uploaded) > p.Progress*float64(totalSize)+min(300*1000*1000, float64(totalSize)*0.1) {
			return true
		}
		return false
	}
}
