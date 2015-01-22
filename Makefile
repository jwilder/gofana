TAG:=`git describe --abbrev=0 --tags`
LDFLAGS:=-X main.buildVersion $(TAG)
GRAFANA_VERSION=1.9.1

.SILENT : grafana-$(GRAFANA_VERSION).tar.gz
.PHONY : gofana clean run

all: gofana

grafana-$(GRAFANA_VERSION).tar.gz:
	wget http://grafanarel.s3.amazonaws.com/grafana-$(GRAFANA_VERSION).tar.gz

grafana-$(GRAFANA_VERSION): grafana-$(GRAFANA_VERSION).tar.gz
	tar xvzf grafana-$(GRAFANA_VERSION).tar.gz

download-grafana: grafana-$(GRAFANA_VERSION)

gofana: download-grafana
	echo "Building gofana"
	go-bindata -o templates.go templates/
	go-bindata -o grafana/grafana.go -pkg grafana grafana-$(GRAFANA_VERSION)/...
	go build -ldflags "$(LDFLAGS)"

dist-clean:
	rm -rf dist
	rm -f gofana-linux-*.tar.gz
	rm -f gofana-darwin-*.tar.gz

dist: dist-clean
	mkdir -p dist/linux/amd64 && GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/linux/amd64/gofana
	mkdir -p dist/darwin/amd64 && GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/darwin/amd64/gofana


release: dist
	glock sync github.com/jwilder/gofana
	tar -cvzf gofana-linux-amd64-$(TAG).tar.gz -C dist/linux/amd64 gofana
	tar -cvzf gofana-darwin-amd64-$(TAG).tar.gz -C dist/darwin/amd64 gofana

run: gofana
	./gofana