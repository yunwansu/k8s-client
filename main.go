package main

import (
	"fmt"
	"io"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"net/http/httptest"
	"path/filepath"
)

func main() {
	var kubeconfig string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	} else {
		kubeconfig = ""
	}

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	req := httptest.NewRequest("GET", "/api/v1/namespaces/kube-system/pods", nil)
	writer := httptest.NewRecorder()

	server := NewServer(config)
	server.ServeHTTP(writer, req)

	resp := writer.Result()
	body, _ := io.ReadAll(resp.Body)

	fmt.Println(resp.StatusCode)
	fmt.Println(resp.Header)
	fmt.Println(string(body))
}
