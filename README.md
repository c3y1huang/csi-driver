# Sample CSI Driver
The purpose of this branch is to help me understand how CSI works with Kubernetest with hands on implementation. 

The driver codes is mostly taken from `kubernetes-csi`.

## Hostpath Plugin
Ref: 
* https://github.com/kubernetes-csi/drivers/tree/master/pkg/csi-common
* https://github.com/kubernetes-csi/csi-driver-host-path/
* https://github.com/kubernetes-csi/drivers

### CSI

Ref: 
https://github.com/container-storage-interface/spec/blob/master/lib/go/csi/csi.pb.go
* csi.NewControllerClient(/*grpc.ClientConn*/)
* csi.NewIdentityClient(/*grpcClientConn*/)
* csi.NewNodeClient(/*grpc.ClientConn*/)

## Usage:

### Build Binary
```
$ make hostpath
```

### Start Driver
```
$ sudo ./hostpathplugin --endpoint tcp://127.0.0.1:10000 -nodeid CSINode -v=5
```

### Test using csc
Get ```csc``` tool from https://github.com/rexray/gocsi/tree/master/csc
```
$ GO111MODULE=off go get -u github.com/rexray/gocsi/csc
```

#### Get plugin info
```
$ csc identity plugin-info --endpoint tcp://127.0.0.1:10000
```

#### Get NodeInfo
```
$ csc node get-info --endpoint tcp://127.0.0.1:10000
```

#### Create Volume
```
$ csc controller new --endpoint tcp://127.0.0.1:10000 --cap MULTI_NODE_MULTI_WRITER,mount,xfs,uid=500 <CSIVolumeName>
<CSIVolumeID> # Example: 9c4c997a-4d4c-11ea-84cd-525400663918 
```

#### Publish Volume
```
$ csc node publish --endpoint tcp://127.0.0.1:10000 --cap MULTI_NODE_MULTI_WRITER,mount,xfs,uid=500 --target-path /mnt/hostpath <CSIVolumeID>
<CSIVolumeID>
```

#### Unpublish Volume
```
$ csc node unpublish --endpoint tcp://127.0.0.1:10000 --target-path /mnt/hostpath <CSIVolumeID>
<CSIVolumeID>
```

#### Delete Volume
```
$ csc controller del --endpoint tcp://127.0.0.1:10000 <CSIVolumeID>
<CSIVolumeID>
```

#### List Volume
```
$ csc controller list-volumes --endpoint tcp://127.0.0.1:10000
```

## Kubernetes - Sizecard Containers

### Driver-registrar
* register the CSI driver with kubelet
* adds the driver custom NodeID to a lable on the Kubernetes Node API Object.
* deploy `plugin` here.

### External-provisioner
* watches Kubernetes PersistentVolumeClaim objects and triggers CreateVolume/DeleteVolume agains a CSI endpoint

### External-attacher
* watches Kubernetes VolumeAttachment objects and triggers ControllerPublish/Unpublish against a CSI endpoint

### Deploy
```
$ kubectl create -f ./manifest/.
```

### Verify
1. Check CSI resources are `Running`.
```
kubectl -n kube-system get pods | grep csi
csi-hostpath-attacher-0                               1/1     Running   0          47m
csi-hostpath-provisioner-0                            1/1     Running   0          47m
csi-hostpath-registrar-0                              2/2     Running   0          47m
```
2. Deploy sample resources.
```
$ kubectl create -f ./manifest/sample/storage-class.yaml
$ kubectl create -f ./manifest/sample/persistent-volume-claim.yaml
$ kubectl create -f ./manifest/sample/pod.yaml
```
3. Check sample `Persistent Volume Claim` bonded to storage class.
```
$ kubectl -n kube-system get pvc
NAME      STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS      AGE
csi-pvc   Bound    pvc-2ba7ef84-770b-40bc-b9b5-af2e7c120b39   1Gi        RWO            csi-hostpath-sc   8m51s
```
4. Check sample `Pod` running.
```
$ kubectl -n kube-system get pod sample
NAME     READY   STATUS    RESTARTS   AGE
sample   1/1     Running   0          65s
```
5. Create `hello.txt` file in sample `Pod`.
```
$ kubectl -n kube-system exec -it sample -- sh -c "echo 'I am here' > /data/hello.txt"
$ kubectl -n kube-system exec -it sample -- sh -c "cat /data/hello.txt"
I am here
```
6. Check volume attached to registrar(plugin) container.
```
$ POD=`kubectl -n kube-system get pods --selector app=csi-hostpath-registrar -o jsonpath='{.items[*].metadata.name}'`
$ kubectl -n kube-system exec -it ${POD} -c hostpath -- sh -c "find / -name hello.txt"
/var/lib/kubelet/pods/2d2e2a9f-7789-4f28-ae5f-414f53334751/volumes/kubernetes.io~csi/pvc-2ba7ef84-770b-40bc-b9b5-af2e7c120b39/mount/hello.txt
/csi-data-dir/61281e9c-4e11-11ea-8b84-fa9b3842591d/hello.txt
```
7. Check with `VolumeAttachment` object is created.
```
$ kubectl describe volumeattachment
Name:         csi-dff51584a67e081d7fb91ced0d2e485ca78c27f2307357256947e1001711e922
Namespace:
Labels:       <none>
Annotations:  <none>
API Version:  storage.k8s.io/v1
Kind:         VolumeAttachment
Metadata:
  Creation Timestamp:  2020-02-13T03:42:34Z                             
  Resource Version:    186626
  Self Link:           /apis/storage.k8s.io/v1/volumeattachments/csi-dff51584a67e081d7fb91ced0d2e485ca78c27f2307357256947e1001711e922            
  UID:                 4f63316d-2234-4506-a61b-f7cbf823b8f6             
Spec:
  Attacher:   hostpath.csi.k8s.io
  Node Name:  caasp-worker-c3y1-cluster-0                               
  Source:
    Persistent Volume Name:  pvc-2ba7ef84-770b-40bc-b9b5-af2e7c120b39   
Status:
  Attached:  true
Events:      <none>
```

## QA

### Where is plugin deployed to 
Plugin for this example is build to single binary and deployed in `registrar`.

## Unfinished Business
* Go through [csi-api](https://github.com/kubernetes/csi-api) to understand how Kubernetes implement CSI CRD.
* Go through [external-provisioner](https://github.com/kubernetes-csi/external-provisioner), [external-snapshotter](https://github.com/kubernetes-csi/external-snapshotter), [external-attacher](https://github.com/kubernetes-csi/external-attacher) to understand how each sidecar works.
