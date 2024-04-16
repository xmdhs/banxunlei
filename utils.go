package main

import (
	"fmt"
	"net/netip"
)

func getPrefix(ip string) (netip.Prefix, error) {
	ipa, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("getPrefix: %w", err)
	}
	if ipa.Is6() {
		return ipa.Prefix(48)
	}
	return ipa.Prefix(24)
}
