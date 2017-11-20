# Slides

Slides content is in the [presentations](https://github.com/giantswarm/presentations/tree/master/content/operators-workshop) repo.

## Exporting slides to PDF

Uses [astefanutti/decktape](https://github.com/astefanutti/decktape).

### Start presentations web server

```bash
$ git clone https://github.com/giantswarm/presentations.git && cd presentations
make serve
```

### Export slides

```bash
WEB_SERVER_IP=*** IP of presentations docker container ***

docker run --rm -v `pwd`:/slides astefanutti/decktape reveal -s 1200x800 http://${WEB_SERVER_IP}:8000/operators-workshop/00-Operators-CRDs /slides/00-Operators-CRDs.pdf
docker run --rm -v `pwd`:/slides astefanutti/decktape reveal -s 1200x800 http://${WEB_SERVER_IP}:8000/operators-workshop/01-Exercise1-REST-API /slides/01-Exercise1-REST-API.pdf
docker run --rm -v `pwd`:/slides astefanutti/decktape reveal -s 1200x800 http://${WEB_SERVER_IP}:8000/operators-workshop/02-Exercise-2-client-go /slides/02-Exercise-2-client-go.pdf
```
