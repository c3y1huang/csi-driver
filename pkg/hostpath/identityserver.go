package hostpath

import (
	"golang.org/x/net/context"
	// "fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IdentityServer is object for identity server
type IdentityServer struct {
	name string
	version string
}

// NewIdentityServer returns helper object of identiy server
func NewIdentityServer(name, version string) *IdentityServer {
	return &IdentityServer{
		name: name,
		version: version,
	}
}

// GetPluginInfo need to return the version and name of the plugin
func (ids *IdentityServer) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	glog.V(5).Infof("Using default GetPluginInfo")

	if ids.name == "" {
		return nil, status.Error(codes.Unavailable, "Driver name not configured")
	}

	if ids.version == "" {
		return nil, status.Error(codes.Unavailable, "Driver is missing version")
	}

	return &csi.GetPluginInfoResponse{
		Name: ids.name,
		VendorVersion: ids.version,
	}, nil
}

// Probe is called by the Container Orchestration(K8s...) just to check whether
// the plugin is running or not. This method doesn't need to return anything. 
// Currently the spec doesn't dictate what you should return either. Hence
// return an empty response.
func (ids *IdentityServer) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

// GetPluginCapabilities returns the capabilities of the plugin. Currently
// it reports whether the plugin has the ability of server the Controller 
// interface. The Container Orchestration(K8s...) calls the Controller interface
// methods depending on whether this method returns the capability or not.
func (ids *IdentityServer) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	glog.V(5).Infof("Using default capabilities")
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}, nil
}
