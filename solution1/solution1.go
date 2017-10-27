package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
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
	"time"
)

// Notes:
// - with API objects has to be created with JSON format even though kbuectl
//   allows YAML

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
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

		if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
			log.Printf("creating custom resource: created")
		} else {
			alreadyExists := false
			errStr := "bad status"

			body := readerToBytesTrimSpace(res.Body)

			// Check if already exists.
			if res.StatusCode == http.StatusConflict {
				r, err := isStatusAlreadyExists(body)
				if err != nil {
					errStr = err.Error()
				} else {
					alreadyExists = r
				}
			}

			if alreadyExists {
				log.Printf("creating custom resource: created")
			} else {
				return fmt.Errorf("creating custom resource: %s status=%d body=%#q", errStr, res.StatusCode, body)
			}
		}
	}

	// Wait for the Custom Resource to be ready.
	{
		attempt := 1
		maxAttempts := 10
		checkInterval := time.Millisecond * 200

		for ; ; attempt++ {
			log.Printf("checking custom resource readiness attempt=%d", attempt)

			res, err := k8sClient.Get(
				config.K8sServer + "/apis/containerconf.de/v1/mysqlconfigs",
			)
			if err != nil {
				return err
			}
			defer res.Body.Close()

			if res.StatusCode == http.StatusOK {
				log.Printf("checking custom resource readiness attempt=%d: ready", attempt)
				break
			}

			if attempt == maxAttempts {
				return fmt.Errorf("checking custom resource readiness attempt=%d: bad status status=%d body=%#q", attempt, res.StatusCode, readerToBytesTrimSpace(res.Body))
			}

			time.Sleep(checkInterval)
		}
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

func readerToBytesTrimSpace(r io.Reader) []byte {
	buf := new(bytes.Buffer)
	buf.ReadFrom(r)
	b := buf.Bytes()
	b = bytes.TrimSpace(b)
	return b
}

func isStatusAlreadyExists(body []byte) (bool, error) {
	m := make(map[string]interface{})
	err := json.Unmarshal(body, &m)
	if err != nil {
		return false, fmt.Errorf("creating custom resource: %s: decoding body=%s", err, body)
	}
	if m["kind"] != "Status" {
		return false, nil
	}
	return m["reason"] == "AlreadyExists", nil
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
