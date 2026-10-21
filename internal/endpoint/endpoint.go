// Package endpoint decides which S3 endpoint addresses ghr accepts.
package endpoint

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const (
	MsgForm     = "the endpoint must be https://host[:port], or http://ip[:port] for a private, loopback or link-local IP address"
	MsgExtra    = "the endpoint must be only https://host[:port] or http://ip[:port], without a path, query or user info"
	MsgZone     = "an IPv6 zone is not supported"
	MsgHTTPName = "use https:// for a host name; http:// is accepted only for a private, loopback or link-local IP address"
	MsgHTTPAddr = "http:// is accepted only for a private, loopback or link-local IP address; use https:// for this address"
)

// Check returns nil when raw is an endpoint ghr accepts, and otherwise an
// error whose text is exactly one of the Msg constants.
func Check(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.Hostname() == "" ||
		strings.HasSuffix(u.Host, ":") || !validPort(u.Port()) {
		return errors.New(MsgForm)
	}
	// url.Parse leaves an empty fragment unset, so "#" is looked for in raw.
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return errors.New(MsgExtra)
	}
	host := u.Hostname()
	if strings.Contains(host, "%") {
		return errors.New(MsgZone)
	}
	if u.Scheme == "https" {
		return nil
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return errors.New(MsgHTTPName)
	}
	// Unmap so an IPv4-mapped IPv6 address is judged by its IPv4 address.
	a := addr.Unmap()
	if a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return nil
	}
	return errors.New(MsgHTTPAddr)
}

// validPort reports whether p is empty or a port from 1 to 65535; url.Parse
// checks only that a port is all digits.
func validPort(p string) bool {
	if p == "" {
		return true
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}
