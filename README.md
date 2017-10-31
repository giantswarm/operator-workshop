# operator-workshop

## Create Postgres deployment

```bash
$ kubectl apply -f postgres.yaml
```

## Run postgresops

```bash
$ go run main.go
```

## Delete Postgres deployment

```bash
$ kubectl delete -f postgres.yaml
```

## Connect to database (optional)

Set password env var from secret.

```bash
$ PGPASSWORD=$(kubectl get secret workshop-postgres -o jsonpath="{.data.postgres-password}" | base64 --decode; echo)
```

Start psql client.

```bash
$ kubectl run workshop-psql-client --rm --tty -i --image postgres \
   --env "PGPASSWORD=$PGPASSWORD" \
   --command -- psql -U postgres \
   -h workshop-postgres postgres
```

Delete psql client.

```bash
$ kubectl delete deploy workshop-psql-client
```
