# operator-workshop

## Setup

During the workshop the operator we create will manage PostgreSQL databases.
A Kubernetes deployment and service is provided to run PostgreSQL in Minikube.

Start a minikube.

```bash
minikube start --kubernetes-version 'v1.8.0'
```

Start PostgreSQL instance inside Kubernetes.

```bash
kubectl apply -f postgresql.yaml
```

## Running Example Solutions

First follow the steps in Setup section.

### Remote Mode

Remote mode means operator runs outside the Kubernetes cluster and connects to
the remote Kubernetes API.

```bash
name="solution1"
#name="solution2"
#name="solution3"

go run ./cmd/${name}/main.go \
    -postgresql.host="$(minikube ip)" \
    -postgresql.port="$(minikube service workshop-postgresql --format '{{.Port}}')" \
    -postgresql.user="postgres" \
    -postgresql.password="operator-workshop" \
    -kubernetes.incluster="false" \
    -kubernetes.server="https://$(minikube ip):8443" \
    -kubernetes.crt="$HOME/.minikube/apiserver.crt" \
    -kubernetes.key="$HOME/.minikube/apiserver.key" \
    -kubernetes.ca="$HOME/.minikube/ca.crt"
```

### In-cluster Mode

Alternative, and most likely desired way to run the operator is the in-cluster
mode. In that mode operator connects to the Kubernetes API with credentials
injected by the Kubernetes to the Pod.

Only solution1 and solution2 support this mode.

To run in in-cluster mode it is necessary to have a docker image in a registry
visible by the cluster and then create a deployment. When using minikube you can
reuse minikube's docker registry.

```bash
eval $(minikube docker-env)

name="solution2"
#name="solution3"

CGO_ENABLED=0 GOOS=linux go build -o operator-workshop ./cmd/${name}/main.go

docker build --tag operator-workshop .

kubectl apply -f ./deployment.yaml
```

## Working with the Operator

### Creating an Example Custom Object

```bash
kubectl apply -f ./example_cro.yaml
```

### List databases

List the PostgreSQL databases and their owners.

```bash
 kubectl exec  \
    $(kubectl get pod -l 'app=workshop-postgresql' -o jsonpath='{.items[0].metadata.name}') \
    -- psql -U postgres postgres -c "\list"
```

### Delete PostgreSQL database

Delete the k8s resources.

```bash
kubectl delete -f postgresql.yaml
```

### Connect to the database

```bash
kubectl exec -it \
    $(kubectl get pod -l 'app=workshop-postgresql' -o jsonpath='{.items[0].metadata.name}') \
    -- psql -U postgres postgres
```
