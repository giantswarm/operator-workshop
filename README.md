# operator-workshop

## Setup

During the workshop the operator we create will manage PostgreSQL databases.
A Kubernetes deployment and service is provided to run PostgreSQL in Minikube.

## Create PostgreSQL database

Create the k8s resources.

```bash
$ kubectl apply -f postgresql.yaml
```

## List databases

List the PostgreSQL databases and their owners.

```bash
$ kubectl exec  \
  $(kubectl get pod -l 'app=workshop-postgresql' -o jsonpath='{.items[0].metadata.name}') \
  -- psql -U postgres postgres -c "\list"
```

## Delete PostgreSQL database

Delete the k8s resources.

```bash
$ kubectl delete -f postgresql.yaml
```

## Connect to the database (optional)

```bash
$ kubectl exec -it \
  $(kubectl get pod -l 'app=workshop-postgresql' -o jsonpath='{.items[0].metadata.name}') \
  -- psql -U postgres postgres
```
