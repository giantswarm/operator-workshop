# operatorkit-workshop solution 1

Easiest way to run the solution is to use minikube.

```bash
minikube start --kubernetes-version 'v1.8.0'
```

Then start the operator in a remote mode. This means operator runs outside the
Kubernetes cluster and connects to the remote kubernetes API.

```bash
go run ../cmd/solution1/main.go
```

There is an example custom object which can be used to test the operator work.

```bash
kubectl apply -f ../example_cro.yaml
```
