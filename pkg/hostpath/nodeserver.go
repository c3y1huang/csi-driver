package hostpath

import (
	"os"
	"fmt"
	"strings"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/kubernetes/pkg/util/mount"
	"k8s.io/kubernetes/pkg/volume/util/volumepathhandler"
)

const TopologyKeyNode = "topology.hostpath.csi/node"

type nodeServer struct {
	nodeID  			string
	ephemeral 			bool
	maxVolumePerNode 	int64
}

// NewNodeServer returns as helper object
func NewNodeServer(nodeId string, ephemeral bool, maxVolumesPerNode int64) *nodeServer {
	return &nodeServer{
		nodeID:	nodeId,
		ephemeral: ephemeral,
		maxVolumePerNode: maxVolumesPerNode,
	}
}

// NodePublishVolume is called to mount the volume from staging to target path. Usually what you do here is a 
// bind mount. A bind mount allows you to mount a path to a different path (instead of mounting a device to
// a path). In Kubernetes, this allows us for example to use the mounted volume from the staging path (i.e
// global directory) to the targte path (pod directory). 
// Here, formatting is not needed because we already did it in NodeStageVolume.
func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	// Check arguments
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	targetPath := req.GetTargetPath()
	ephemeralVolume := req.GetVolumeContext()["csi.storage.k8s.io/ephemeral"] == "true" ||
		req.GetVolumeContext()["csi.storage.k8s.io/ephemeral"] == "" && ns.ephemeral // Kubernetes 1.15 doesn't have csi.storage.k8s.io/ephemeral
	
	if req.GetVolumeCapability().GetBlock() != nil &&
		req.GetVolumeCapability().GetMount() != nil {
		return nil, status.Error(codes.InvalidArgument, "cannot have both block and mount access type")
	}

	// if ephemal is specified, create volume here to avoid errors
	if ephemeralVolume {
		volID := req.GetVolumeId()
		volName := fmt.Sprintf("ephemeral=%s", volID)
		vol, err := createHostpathVolume(req.GetVolumeId(), volName, maxStorageCapacity, mountAccess, ephemeralVolume)
		if err != nil && !os.IsExist(err) {
			glog.Error("ephemeral mode failed to create volume: ", err)
			return nil, status.Error(codes.Internal, err.Error())
		}
		glog.V(4).Infof("ephemeral mode: created volume: %s", vol.VolPath)
	}

	vol, err := getVolumeByID(req.GetVolumeId())
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	if req.GetVolumeCapability().GetBlock() != nil {
		if vol.VolAccessType != blockAccess {
			return nil, status.Error(codes.InvalidArgument, "cannot publish a non-block volume as block volume")
		}

		volPathHandler := volumepathhandler.VolumePathHandler{}

		// Get loop device from the volume path
		loopDevice, err := volPathHandler.GetLoopDevice(vol.VolPath)
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get the loop device: %v", err))
		}

		mounter := mount.New("")

		// Check if the target path exists. Create if not present.
		_, err = os.Lstat(targetPath)
		if os.IsNotExist(err) {
			// if err = mounter.MakeFile(targetPath); err != nil {
			if err = makeFile(targetPath); err != nil {
				return nil, status.Error(codes.Internal, fmt.Sprintf("failed to create target path: %s: %v", targetPath, err))	
			}
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to check if the target block file exists: %v", err)
		}
		
		// Check if the target path is already mounted. Prevent remounting
		// notMount, err := mounter.IsNotMountPoint(targetPath)
		notMount, err := mount.IsNotMountPoint(mounter, targetPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, status.Errorf(codes.Internal, "error checking path %s for mount: %s", targetPath, err)
			}
			notMount = true
		}
		if !notMount {
			// Its already mounted.
			glog.V(5).Infof("Skipping bind-mounting subpath %s: already mounted", targetPath)
			return &csi.NodePublishVolumeResponse{}, nil
		}

		options := []string{"bind"}
		if err := mount.New("").Mount(loopDevice, targetPath, "", options); err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to mount block device: %s at %s: %v", loopDevice, targetPath, err))
		}
	} else if req.GetVolumeCapability().GetMount() != nil {
		if vol.VolAccessType != mountAccess {
			return nil, status.Error(codes.InvalidArgument, "cannot publish a non-mount volume as mount volume")
		}

		mounter := mount.New("")
		notMnt, err := mount.IsNotMountPoint(mounter, targetPath)
		if err != nil {
			if os.IsNotExist(err) {
				if err = os.MkdirAll(targetPath, 0750); err != nil {
					return nil, status.Error(codes.Internal, err.Error())
				}
				notMnt = true
			} else {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}

		if !notMnt {
			return &csi.NodePublishVolumeResponse{}, nil
		}

		fsType := req.GetVolumeCapability().GetMount().GetFsType()

		deviceId := ""
		if req.GetPublishContext() != nil {
			deviceId = req.GetPublishContext()[deviceID]
		}

		readOnly := req.GetReadonly()
		volumeId := req.GetVolumeId()
		attrib := req.GetVolumeContext()
		mountFlags := req.GetVolumeCapability().GetMount().GetMountFlags()

		glog.V(4).Infof("target %v", targetPath)
		glog.V(4).Infof("fstype %v", fsType)
		glog.V(4).Infof("device %v", deviceId)
		glog.V(4).Infof("readonly %v", readOnly)
		glog.V(4).Infof("volumeId %v", volumeId)
		glog.V(4).Infof("attributes %v", attrib)
		glog.V(4).Infof("mountflags %v", mountFlags)

		options := []string{"bind"}
		if readOnly {
			options = append(options, "ro")
		}
		// mounter := mount.New("")
		path := getVolumePath(volumeId)
		if err := mounter.Mount(path, targetPath, "", options); err != nil {
			var errList strings.Builder
			errList.WriteString(err.Error())
			if vol.Ephemeral {
				if rmErr := os.RemoveAll(path); rmErr != nil && !os.IsNotExist(rmErr) {
					errList.WriteString(fmt.Sprintf(" :%s", rmErr.Error()))
				}
			}
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to mount block device: %s at %s: %s", path, targetPath, errList.String()))
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil
}


// NodeUnpublishVolume is the reverse of NodePublishVolume. It unmounts the volume from the target path.
func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	targetPath := req.GetTargetPath()
	volumeID := req.GetVolumeId()

	vol, err := getVolumeByID(volumeID)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Unmount only if the target path is really a mount point.
	if notMnt, err := mount.IsNotMountPoint(mount.New(""), targetPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else if !notMnt {
		// Unmounting the image or filesystem.
		err = mount.New("").Unmount(targetPath)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	// Delete the mount point.
	// Does not return error for non-existent path, repeat calls OK for idempotency.
	if err = os.RemoveAll(targetPath); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	glog.V(4).Infof("hostpath: volume %s has been unpublished.", targetPath)

	if vol.Ephemeral {
		glog.V(4).Infof("deleting volume %s", volumeID)
		if err := deleteHostpathVolume(volumeID); err != nil && !os.IsNotExist(err) {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to delete volume: %s", err))
		}
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeStageVolume is called by Controller Orchestrator to temporarily mount the volume to a staging path.
// Usually this staging path is a global directory on the node. In Kubernetes, after it's mounted to the 
// global directory, you mount it into the pod directory (via NodePublishVolume). The reason that mounting
// is a two step operation is because Kubernetes allows you to use a single volume by multiple pods. This
// allowed when the storage system supports it (say NFS) or if a ll pods run on the same node. One thing
// to note is that you also need to format the volume if it's not formatted already. Keep that in mind.
func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetStagingTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capability missing in request")
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume is the reverse of NodeStageVolume. Called by Controller Orchestrator to unmount the
// volume from the staging path.
func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetStagingTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missin gin request")
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodeGetInfo returns the supported capabilities of the node server. This should 
// eventually return the droplet ID if possible. This is sused so the Controller 
// Orchestrator knows where to place the workload. The result of this function will
// be used by the Controller Orchestrator in the ControllerPublishVolume.
func (ns *nodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	topology := &csi.Topology{
		Segments: map[string]string{TopologyKeyNode: ns.nodeID},
	}

	return &csi.NodeGetInfoResponse{
		NodeId: 	ns.nodeID,
		MaxVolumesPerNode: ns.maxVolumePerNode,
		AccessibleTopology: topology,
	}, nil
}

// NodeGetCapabilities returns the capabilities of the Node plugin. For example, if you don't advertise
// RPC_STAGE_UNSTAGE_VOLUME capabilities, the Controller Orchestrator will not call NodeStageVolume and
// NodeUnstageVolume as you don't provide it.
func (ns *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (ns *nodeServer) NodeGetVolumeStats(ctx context.Context, in *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (ns *nodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	vol, err := getVolumeByID(volID)
	if err != nil {
		// Assume not found error
		return nil, status.Errorf(codes.NotFound, "Cound not get volume %s: %v", volID, err)
	}

	volPath := req.GetVolumePath()
	if len(volPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume path not provided")
	}

	info, err := os.Stat(volPath)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Could not get file information from %s: %v", volPath, err)
	}

	switch m := info.Mode(); {
	case m.IsDir():
		if vol.VolAccessType != mountAccess {
			return nil, status.Errorf(codes.InvalidArgument, "Volume %s is not a directory", volID)
		}
	case m&os.ModeDevice != 0:
		if vol.VolAccessType != blockAccess {
			return nil, status.Errorf(codes.InvalidArgument, "Volume %s is invalid", volID)
		}
	default:
		return nil, status.Errorf(codes.InvalidArgument, "Volume %s is invalid", volID)
	}

	return &csi.NodeExpandVolumeResponse{}, nil
}