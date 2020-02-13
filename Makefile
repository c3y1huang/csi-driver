REV=$(shell git describe --long --tags --dirty)

all: hostpath

hostpath: 
	if [ ! -d ./vendor ]; then dep ensure -vendor-only; fi
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-X github.com/c3y1huang/csi-driver/pkg/hostpath.vendorVersion=$(REV) -extldflags "-static"' -o _output/hostpathplugin ./cmd/hostpathplugin
