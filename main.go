package main

import (
	"flag"
	"k8s-client/pkg/server"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"os"
	"path/filepath"
)

func main() {
	fs := flag.NewFlagSet("", flag.ExitOnError)
	fPort := fs.Int("port", 8080, "http listen port")
	fs.Parse(os.Args[1:])

	config, err := getConfig()
	if err != nil {
		panic(err.Error())
	}

	s, err := server.NewServer(*fPort, config)
	if err != nil {
		panic(err.Error())
	}
	s.Run()
}

func getConfig() (*rest.Config, error) {
	kubeconfigPath := ""
	if home := homedir.HomeDir(); home != "" {
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}
