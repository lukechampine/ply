ldflags = -X main.githash=`git rev-parse HEAD` \
          -X main.version=0.1.0 \
          -X 'main.builddate=`date`'

# all builds a binary with the current commit hash
all:
	go install -ldflags "$(ldflags)" ./...

lint:
	go vet ./...
