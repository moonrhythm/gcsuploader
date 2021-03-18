COMMIT_SHA=$(shell git rev-parse HEAD)
IMAGE=gcr.io/moonrhythm-containers/gcsuploader

build:
	buildctl build \
		--frontend dockerfile.v0 \
		--local dockerfile=. \
		--local context=. \
		--output type=image,name=$(IMAGE):$(COMMIT_SHA),push=true
