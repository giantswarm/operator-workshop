package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path"
	"strconv"
	"strings"

	"github.com/giantswarm/operator-workshop/solution1"
)

const (
	dbServiceDefault  = "workshop-postgresql"
	dbUserDefault     = "postgres"
	dbPasswordDefault = "operator-workshop"
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("I ")
}

func main() {
	ctx := context.Background()

	config := parseFlags()

	mainExitCodeCh := make(chan int)
	mainCtx, mainCancelFunc := context.WithCancel(ctx)

	// Run actual code.
	go func() {
		err := solution1.Run(mainCtx, config)
		if err != nil {
			log.SetPrefix("E ")
			log.Printf("%s", err)
			mainExitCodeCh <- 1
		}
		mainExitCodeCh <- 0
	}()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, os.Kill)

	// Handle graceful stop.
	gracefulStop := false
	for {
		select {
		case code := <-mainExitCodeCh:
			log.Printf("exiting: code=%d", code)
			os.Exit(code)
		case sig := <-sigCh:
			// On second SIGKILL exit immediately.
			if sig == os.Kill && gracefulStop {
				log.Printf("exiting: forced exit code=1")
				os.Exit(1)
			}
			if !gracefulStop {
				log.Printf("exiting: trying to preform graceful stop")
				gracefulStop = true
				mainCancelFunc()
			}
		}
	}
}

func parseFlags() solution1.Config {
	var config solution1.Config

	var homeDir string
	{
		u, err := user.Current()
		if err != nil {
			homeDir = os.Getenv("HOME")
		} else {
			homeDir = u.HomeDir
		}

	}

	var minikubeIP string
	{
		out, err := exec.Command("minikube", "ip").Output()
		if err == nil {
			minikubeIP = strings.TrimSpace(string(out))
		}
	}

	var serverDefault string
	{
		if minikubeIP != "" {
			serverDefault = "https://" + string(minikubeIP) + ":8443"
		}
	}

	var dbPortDefault int
	{
		out, err := exec.Command("minikube", "service", dbServiceDefault, "--format", "{{.Port}}").Output()
		if err == nil {
			s := strings.TrimSpace(string(out))
			dbPortDefault, err = strconv.Atoi(s)
			if err != nil {
				dbPortDefault = 0
			}
		}
	}

	flag.StringVar(&config.DBHost, "postgresql.host", minikubeIP, "PostgreSQL server host.")
	flag.IntVar(&config.DBPort, "postgresql.port", dbPortDefault, "PostgreSQL server port.")
	flag.StringVar(&config.DBUser, "postgresql.user", dbUserDefault, "PostgreSQL user.")
	flag.StringVar(&config.DBPassword, "postgresql.password", dbPasswordDefault, "PostgreSQL password.")
	flag.BoolVar(&config.K8sInCluster, "kubernetes.incluster", false, "Run inside Kubernets cluster.")
	flag.StringVar(&config.K8sServer, "kubernetes.server", serverDefault, "Kubernetes API server address.")
	flag.StringVar(&config.K8sCrtFile, "kubernetes.crt", path.Join(homeDir, ".minikube/apiserver.crt"), "Kubernetes certificate file path.")
	flag.StringVar(&config.K8sKeyFile, "kubernetes.key", path.Join(homeDir, ".minikube/apiserver.key"), "Kubernetes key file path.")
	flag.StringVar(&config.K8sCAFile, "kubernetes.ca", path.Join(homeDir, ".minikube/ca.crt"), "Kubernetes CA file path.")
	flag.Parse()

	return config
}
