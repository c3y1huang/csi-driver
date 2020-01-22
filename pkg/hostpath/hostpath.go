package hostpath

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/kubernetes/pkg/volume/util/volumepathhandler"
	utilexec "k8s.io/utils/exec"

	timestamp "github.com/golang/protobuf/ptypes/timestamp"
)

const (
	kib  	int64 = 1024
	mib 	int64 = kib * 1024
	gib 	int64 = mib * 1024
	tib 	int64 = gib * 1024
)

type hostPath struct {
	name    			string
	nodeID  			string
	version 			string
	endpoint 			string
	ephemeral 			bool
	maxVolumesPerNode 	int64

	ids *IdentityServer
	ns 	*nodeServer
	cs 	*controllerServer
}

type hostPathVolume struct {
	VolName 		string  	`json:"volName"`
	VolID 			string		`json:"volID"`
	VolSize			int64		`json:"volSize"`
	VolPath			string		`json:"volPath"`
	VolAccessType	accessType 	`json:"volAccessType"`
	Ephemeral		bool		`json:"ephemeral"`
}

type hostPathSnapshot struct {
	Name  			string					`json:"name"`
	ID				string 					`json:"id"`
	VolID  			string      			`json:"volID"`
	Path   			string  				`json:"path"`
	CreationTime 	timestamp.Timestamp  	`json"creationTime"`
	SizeBytes		int64 					`json:"sizeBytes"`
	ReadyToUse      bool 					`json:"readyToUse"`
}

var (
	vendorVersion = "dev"

	hostPathVolumes			map[string]hostPathVolume
	hostPathVolumeSnapshots map[string]hostPathSnapshot
)

const (
	// Directory where data for volume and snapshots are persited.
	// This can be ephemeral within the container or persited if
	// backed by a Pod volume
	dataRoot = "/csi-data-dir"
)

func init() {
	hostPathVolumes = map[string]hostPathVolume{}
	hostPathVolumeSnapshots = map[string]hostPathSnapshot{}
}

func NewHostPathDriver(driverName, nodeID, endpoint string, ephemeral bool, maxVolumesPerNode int64, version string) (*hostPath, error) {
	if driverName == "" {
		return nil, fmt.Errorf("No driver name provided")
	}

	if nodeID == "" {
		return nil, fmt.Errorf("No node id provided")
	}

	if endpoint == "" {
		return nil, fmt.Errorf("No driver endpoint provided")
	}
	
	if version != "" {
		vendorVersion = version
	}

	if err := os.MkdirAll(dataRoot, 0750); err != nil {
		return nil, fmt.Errorf("failed to create dataRoot :%v", err)
	}

	glog.Info("Driver: %v", driverName)
	glog.Info("Version: %v", vendorVersion)

	return &hostPath{
		name: driverName,
		version:	vendorVersion,
		nodeID:	nodeID,
		endpoint: 	endpoint,
		ephemeral: ephemeral,
		maxVolumesPerNode: maxVolumesPerNode,
	}, nil
}

func (hp *hostPath) Run () {
	// Create GRPC servers
	hp.ids = NewIdentityServer(hp.name, hp.version)
	hp.ns = NewNodeServer(hp.nodeID, hp.ephemeral, hp.maxVolumesPerNode)
	hp.cs = NewControllerServer(hp.ephemeral, hp.nodeID)

	s := NewNonBlockingGRPCServer()
	s.Start(hp.endpoint, hp.ids, hp.cs, hp.ns)
	s.Wait()
}

func getVolumeByName(volName string) (hostPathVolume, error) {
	for _, hostPathVol := range hostPathVolumes {
		if hostPathVol.VolName == volName {
			return hostPathVol, nil
		}
	}
	return hostPathVolume{}, fmt.Errorf("volume name %s does not exist in the volume list", volName)
}

func getSnapshotByName(name string) (hostPathSnapshot, error) {
	for _, snapshot := range hostPathVolumeSnapshots{
		if snapshot.Name == name {
			return snapshot, nil
		}
	}
	return hostPathSnapshot{}, fmt.Errorf("snapshot name %s does not exist in the snapshots list")
}

func getVolumeByID(volID string) (hostPathVolume, error) {
	if hostPathVol, ok := hostPathVolumes[volID]; ok {
		return hostPathVol, nil
	}
	return hostPathVolume{}, fmt.Errorf("volume id %s does not exist in the volumes list", volID)
}

func getVolumePath(volID string) string {
	return filepath.Join(dataRoot, volID)
}

// createHostpathVolume crates volume directory on host and returns hostPathVolume 
// object in hostPathVolumes with volID.
func createHostpathVolume(volID string, name string, cap int64, volAccessType accessType, ephemeral bool) (*hostPathVolume, error) {
	path := getVolumePath(volID)

	switch volAccessType {
	case mountAccess:
		err := os.MkdirAll(path, 0777)
		if err != nil {
			return nil, err
		}
	case blockAccess:
		fallocateCmd := "fallocate"
		// TODO: check if cmd exist in $PATH

		executer := utilexec.New()
		size := fmt.Sprintf("%dM", cap/mib)
		args := []string{"-l", size, path}
		// Create a block file
		out, err := executer.Command(fallocateCmd, args...).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("failed to create block device: %v, %v", err, string(out))
		}

		// Assiciate block file with the loop device.
		volPathHandler := volumepathhandler.VolumePathHandler{}
		_, err = volPathHandler.AttachFileDevice(path)
		if err != nil {
			// Remove the block file because it's no longer be used again.
			if err2 := os.Remove(path); err2 != nil {
				glog.Errorf("failed to cleanup block file %s: %v", path, err2)
			}
			return nil, fmt.Errorf("failed to attach device %v: %v", path, err)
		}
	default:
		return nil, fmt.Errorf("unsupported access type %v", volAccessType)
	}

	hostpathVol := hostPathVolume{
		VolID: 			volID,
		VolName: 		name,
		VolSize: 		cap,
		VolPath:		path,
		VolAccessType: 	volAccessType,
		Ephemeral: 	ephemeral,
	}
	hostPathVolumes[volID] = hostpathVol
	return &hostpathVol, nil
}

// updateVolume updates the existing hostpath volume
func updateHostpathVolume(volID string, volume hostPathVolume) error {
	glog.V(4).Infof("updating hostpath volume: %s", volID)

	if _, err := getVolumeByID(volID); err != nil {
		return err
	}

	hostPathVolumes[volID] = volume
	return nil
}

func deleteHostpathVolume(volID string) error {
	glog.V(4).Infof("deleting hostpath volume: %s", volID)

	vol, err := getVolumeByID(volID)
	if err != nil {
		// Return OK if the volume is not found.
		return nil
	}

	if vol.VolAccessType == blockAccess {
		volPathHandler := volumepathhandler.VolumePathHandler{}
		// Get the associated loop device.
		device, err := volPathHandler.GetLoopDevice(getVolumePath(volID))
		if err != nil {
			// Remove any assiciated loop device.
			glog.V(4).Infof("deleting loop device %s", device)
			device, err := volPathHandler.GetLoopDevice(device)
			if err != nil {
				return fmt.Errorf("failed to remove loop decie %v: %v", device, err)
			}
		}
	}

	path := getVolumePath(volID)
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	// remove volID from hostPathVolumes
	delete(hostPathVolumes, volID)
	return nil
}

// loadFromSnapshot populates the given destPath with data from the snapshotID
func loadFromSnapshot(snapshotID string, destPath string) error {
	tarCmd := "tar"
	// TODO: check if exist in $PATH
	snapshot, ok := hostPathVolumeSnapshots[snapshotID]
	if !ok {
		return status.Errorf(codes.NotFound, "cannot find snapshot %v", snapshotID)
	}
	if snapshot.ReadyToUse != true {
		return status.Errorf(codes.Internal, "snapshot %v is not yet ready to use.", snapshotID)
	}
	snapshotPath := snapshot.Path
	args := []string{"zxvf", snapshotPath, "-C", destPath}
	executor := utilexec.New()
	out, err := executor.Command(tarCmd, args...).CombinedOutput()
	if err != nil {
		return status.Errorf(codes.Internal, "failed pre-populate data from snapshot %v: %v: %s", snapshotID, err, out)
	}
	return nil
}

// loadFromVolume populate the given destPath with data from srcVolumeID
func loadFromVolume(srcVolumeID string, destPath string) error {
	cpCmd := "cp"
	hostPathVolume, ok := hostPathVolumes[srcVolumeID]
	if !ok {
		return status.Error(codes.NotFound, "source volumeID does not exist, are source/destination in the same storage class?")
	}
	srcPath := hostPathVolume.VolPath
	isEmpty, err := hostPathIsEmpty(srcPath)
	if err != nil {
		return status.Errorf(codes.Internal, "failed verification check of source hostpath volume: %s: %v", srcVolumeID, err)
	}

	if !isEmpty {
		args := []string{"-a", srcPath + "/.", destPath + "/"}
		executer := utilexec.New()
		out, err := executer.Command(cpCmd, args...).CombinedOutput()
		if err != nil {
			return status.Errorf(codes.Internal, "failed pre-populate data from volume %v: %v: %s", srcVolumeID, err, out)
		}
	}
	return nil
}

func hostPathIsEmpty(p string) (bool, error) {
	f, err := os.Open(p)
	if err != nil {
		return true, fmt.Errorf("unable to open hostpath volume, error: %v", err)
	}
	defer f.Close()

	_, err = f.Readdir(1)
	if err == io.EOF {
		return true, nil
	}
	return false, err
}
