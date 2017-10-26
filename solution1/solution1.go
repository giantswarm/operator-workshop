package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path"
	"strings"
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC | log.Lshortfile)
	log.SetPrefix("I ")
}

type Config struct {
	K8sServer  string
	K8sCrtFile string
	K8sKeyFile string
	K8sCAFile  string
}

func main() {
	config := parseFlags()

	err := mainError(config)
	if err != nil {
		log.SetPrefix("E ")
		log.Printf("%s", err)
		os.Exit(1)
	}
}

func parseFlags() Config {
	var config Config

	var homedir string
	{
		u, err := user.Current()
		if err != nil {
			homedir = os.Getenv("HOME")
		} else {
			homedir = u.HomeDir
		}

	}

	var serverDefault string
	{
		out, err := exec.Command("minikube", "ip").Output()
		if err == nil {
			minikubeIP := strings.TrimSpace(string(out))
			serverDefault = "https://" + string(minikubeIP) + ":8443"
		}
	}

	flag.StringVar(&config.K8sServer, "kubernetes.server", serverDefault, "Kubernetes API server address.")
	flag.StringVar(&config.K8sCrtFile, "kubernetes.crt", path.Join(homedir, ".minikube/apiserver.crt"), "Kubernetes certificate file path.")
	flag.StringVar(&config.K8sKeyFile, "kubernetes.key", path.Join(homedir, ".minikube/apiserver.key"), "Kubernetes key file path.")
	flag.StringVar(&config.K8sCAFile, "kubernetes.ca", path.Join(homedir, ".minikube/ca.crt"), "Kubernetes CA file path.")
	flag.Parse()

	return config
}

func mainError(config Config) error {
	k8sClient, err := newHttpClient(config)
	if err != nil {
		return err
	}

	// Create Custom Resource Definition.
	{
		log.Printf("creating custom resource")

		res, err := k8sClient.Post(
			config.K8sServer+"/apis/apiextensions.k8s.io/v1beta1/customresourcedefinitions",
			"application/json",
			strings.NewReader(crdJson),
		)
		if err != nil {
			return err
		}
		defer res.Body.Close()

		switch res.StatusCode {
		case http.StatusConflict:
			m := make(map[string]interface{})
			err := json.NewDecoder(res.Body).Decode(&m)
			return fmt.Errorf("creating custom resource: decoding body=%s", readerToString(res.Body))
		case http.StatusOK:
			log.Printf("created custom resource")
		default:
			return fmt.Errorf("creating custom resource: bad status status=%d body=%s", res.StatusCode, readerToString(res.Body))
		}
	}

	{
		res, err := k8sClient.Get(
			config.K8sServer + "/apis/apiextensions.k8s.io/v1beta1/customresourcedefinitions",
		)
		if err != nil {
			return err
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return errors.New("creating custom resource: " + readerToString(res.Body))
		}

		log.Print(readerToString(res.Body))
	}

	return nil
}

func newHttpClient(config Config) (*http.Client, error) {
	crt, err := tls.LoadX509KeyPair(config.K8sCrtFile, config.K8sKeyFile)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	caCert, err := ioutil.ReadFile(config.K8sCAFile)
	if err != nil {
		return nil, err
	}
	certPool.AppendCertsFromPEM(caCert)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{crt},
		RootCAs:      certPool,
	}
	tlsConfig.BuildNameToCertificate()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return client, nil
}

func readerToString(r io.Reader) string {
	buf := new(bytes.Buffer)
	buf.ReadFrom(r)
	return buf.String()
}

// crdJson content in YAML format can be found in crd.yaml file.
const crdJson = `{
	"apiVersion": "apiextensions.k8s.io/v1beta1",
	"kind": "CustomResourceDefinition",
	"metadata": {
		"name": "mysqlconfigs.containerconf.de"
	},
	"spec": {
		"group": "containerconf.de",
		"version": "v1",
		"scope": "Namespaced",
		"names": {
			"plural": "mysqlconfigs",
			"singular": "mysqlconfig",
			"kind": "MySQLConfig"
		},
		"shortNames": []
	}
}`
