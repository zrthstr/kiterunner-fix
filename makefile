
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get

GIT_TAG=$(shell git tag -l | head -n1)
GIT_COMMIT_HASH:=$(shell git rev-parse HEAD)
DATE:=$(shell date)
BENCHTEST="BenchmarkKiterunnerEngineRunGolandSmall100$$"
PACKAGE="./benchmark/"
COUNT=5
SHORTBUILDFLAGS=-extld 'g++' -extldflags '-static'
BUILDFLAGS="$(SHORTBUILDFLAGS) -X 'github.com/zrthstr/kiterunner-fix/cmd/kiterunner/cmd.Version=$(GIT_TAG)' -X 'github.com/zrthstr/kiterunner-fix/cmd/kiterunner/cmd.Commit=$(GIT_COMMIT_HASH)' -X 'github.com/zrthstr/kiterunner-fix/cmd/kiterunner/cmd.Date=$(DATE)'"

test:
	$(GOTEST) ./...

# coverage of the unit tests and of the kr binary as exercised by the e2e tests, merged into cover.out
cover:
	rm -rf .cover && mkdir -p .cover/e2e
	KR_E2E_COVERDIR=$(CURDIR)/.cover/e2e $(GOTEST) -count=1 ./e2e/
	$(GOCMD) tool covdata textfmt -i=.cover/e2e -o=.cover/e2e.out
	$(GOTEST) -count=1 -coverpkg=./... -coverprofile=.cover/unit.out ./...
	# generated protobuf code and the benchmark helpers are left out so the total reflects hand written code
	{ echo "mode: set"; tail -q -n +2 .cover/unit.out .cover/e2e.out | grep -v -e '\.pb\.go:' -e '/cmd/testServer/' -e '/benchmark/'; } > cover.out
	$(GOCMD) tool cover -func=cover.out | tail -1
	@echo "html report: go tool cover -html=cover.out"

build:
	mkdir -p dist
	$(GOBUILD) -ldflags $(BUILDFLAGS) -o dist/kr ./cmd/kiterunner

build-linux:
	mkdir -p dist
	GOOS=linux $(GOBUILD) -tags "netgo osusergo" -a -ldflags $(BUILDFLAGS) -o dist/kr-linux ./cmd/kiterunner

build-bench-linux:
	mkdir -p dist
	GOOS=linux $(GOBUILD) -tags "netgo osusergo" -a -ldflags "$(SHORTBUILDFLAGS)" -o dist/benchserv ./cmd/testServer/main.go

bench-mem:
	ulimit -n 20000 && \
		$(GOTEST) $(PACKAGE) -tags=integration -bench=$(BENCHTEST) -count=$(COUNT) -benchmem -run='^$$' -memprofile=/tmp/$(BENCHTEST).mem.pprof | tee ./testing/$(BENCHTEST).mem-benchstat.txt

bench-up:
	ulimit -n 20000 && \
		$(GOCMD) run ./cmd/testServer/main.go -p=14000-15001

increase-ulimit:
	sudo launchctl limit maxfiles 1048576 2048576
	ulimit -n 20000

profile:
	bash hack/profile.sh


gen-proto:
	bash ./hack/gen-proto.sh



