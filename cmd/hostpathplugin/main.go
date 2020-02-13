package main

import (
	"flag"
	"fmt"
	"os"
	"path"

	"github.com/c3y1huang/csi-driver/pkg/hostpath"
)

func init() {
	flag.Set("logtostderr", "true")
}

var (
	endpoint          = flag.String("endpoint", "unix://csi/csi.sock", "CSI endpoint")
	driverName        = flag.String("drivername", "hostpath.csi.k8s.io", "name of the driver")
	nodeID            = flag.String("nodeid", "", "node id")
	ephemeral         = flag.Bool("ephemeral", false, "publish volumes in ephemeral mode even if kubelet did not ask for it (only needed for Kubernetes 1.15)")
	maxVolumesPerNode = flag.Int64("maxvolumespernode", 0, "limit of volumes per node")
	showVersion       = flag.Bool("version", false, "Show version.")
	// Set by the build process
	version = "alpha"
)

func main() {
	flag.Parse()

	if *showVersion {
		baseName := path.Base(os.Args[0])
		fmt.Println(baseName, version)
		return
	}

	if *ephemeral {
		fmt.Fprintln(os.Stderr, "Deprecation waring: The ephemeral flag is deprecated and should only be used when deploying on Kubernetes 1.15. It would be remove in the future.")
	}

	handle()
	os.Exit(0)
}

func handle() {
	driver, err := hostpath.NewHostPathDriver(*driverName, *nodeID, *endpoint, *ephemeral, *maxVolumesPerNode, version)
	if err != nil {
		fmt.Printf("Failed to initialize driver: %s", err.Error())
		os.Exit(1)
	}
	driver.Run()
}
