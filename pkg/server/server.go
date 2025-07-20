package server

import (
	"context"
	"fmt"
	v1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"k8s-client/pkg/resource_filter"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/rest"
	"net/http"
	"strings"
)

type Server struct {
	Port           int
	resourceFilter *resource_filter.ResourceFilter
}

func NewServer(port int, config *rest.Config) (*Server, error) {
	resourceFilter, err := resource_filter.NewResourceFilter(context.TODO(), config)
	if err != nil {
		return nil, err
	}

	return &Server{
		Port:           port,
		resourceFilter: resourceFilter,
	}, nil
}

func (s *Server) Run() {
	mux := http.NewServeMux()
	mux.Handle("/resource-filter", s)
	mux.Handle("/", http.FileServer(http.Dir("assets")))

	server := http.Server{Addr: fmt.Sprintf(":%d", s.Port), Handler: mux}

	server.ListenAndServe()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	includedNamespaces := r.FormValue("includedNamespaces")
	excludedNamespaces := r.FormValue("excludedNamespaces")
	includedResources := r.FormValue("includedResources")
	excludedResources := r.FormValue("excludedResources")
	includeClusterResources := r.FormValue("includeClusterResources")
	b := includeClusterResources == "on"

	backup := v1.Backup{
		Spec: v1.BackupSpec{
			IncludedNamespaces:      strings.Split(includedNamespaces, ","),
			ExcludedNamespaces:      strings.Split(excludedNamespaces, ","),
			IncludedResources:       strings.Split(includedResources, ","),
			ExcludedResources:       strings.Split(excludedResources, ","),
			IncludeClusterResources: &b,
		},
	}

	resources, err := s.resourceFilter.GetFilteredItems(&backup)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	res, err := json.Marshal(resources)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(res)
}
