package main

import (
	"k8s.io/client-go/rest"
	"net/http"
)

type Server struct {
	config *rest.Config
}

func NewServer(config *rest.Config) *Server {
	return &Server{
		config: config,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	proxy, err := NewProxy(s.config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	proxy.ServeHTTP(w, r)
}
