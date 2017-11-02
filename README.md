# operator-workshop

## Create PostgreSQL deployment

```bash
$ kubectl apply -f postgresql.yaml
```

## Run postgresops

```bash
$ go run main.go
```

## Delete PostgreSQL deployment

```bash
$ kubectl delete -f postgresql.yaml
```

## Connect to database (optional)

Set password env var from secret.

```bash
$ PGPASSWORD=$(kubectl get secret workshop-postgresql -o jsonpath="{.data.postgresql-password}" | base64 --decode; echo)
```

Start psql client.

```bash
$ kubectl run workshop-psql-client --rm --tty -i --image postgres \
   --env "PGPASSWORD=$PGPASSWORD" \
   --command -- psql -U postgres \
   -h workshop-postgresql postgres
```

Delete psql client.

```bash
$ kubectl delete deploy workshop-psql-client
```
