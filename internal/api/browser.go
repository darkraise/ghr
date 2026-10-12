package api

import "context"

type browserKey struct{}

// WithBrowser marks ctx as a request the web UI's listener forwarded from a
// browser, with the Host header the browser sent.
func WithBrowser(ctx context.Context, host string) context.Context {
	return context.WithValue(ctx, browserKey{}, host)
}

// Browser reports the Host header of a request the web UI forwarded; ok is
// false for a request over the socket.
func Browser(ctx context.Context) (host string, ok bool) {
	host, ok = ctx.Value(browserKey{}).(string)
	return host, ok
}
