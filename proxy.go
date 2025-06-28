package main

import (
	"k8s.io/client-go/rest"
	"net/http/httputil"
	"net/url"
	"time"
)

type Proxy struct {
	config *rest.Config
	*httputil.ReverseProxy
}

func NewProxy(config *rest.Config) (*Proxy, error) {
	targetUrl, err := url.Parse(config.Host)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(targetUrl)
	proxy.FlushInterval = time.Millisecond * 100
	proxy.Transport, _ = rest.TransportFor(config)

	return &Proxy{config, proxy}, nil
}
