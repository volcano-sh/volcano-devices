# Volcano device plugin for Kubernetes

**Note**:
This is based on [Nvidia Device Plugin](https://github.com/NVIDIA/k8s-device-plugin) to support soft isolation of GPU card.
And collaborate with volcano, it is possible to enable GPU sharing.

## Table of Contents

- [About](#about)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
  - [Preparing your GPU Nodes](#preparing-your-gpu-nodes)
  - [Enabling GPU Support in Kubernetes](#enabling-gpu-support-in-kubernetes)
  - [Running GPU Sharing Jobs](#running-gpu-sharing-jobs)
  - [Running GPU Number Jobs](#running-gpu-number-jobs)
- [Docs](#docs)
- [Issues and Contributing](#issues-and-contributing)


## About

The Volcano device plugin for Kubernetes is a Daemonset that allows you to automatically:
- Expose the number of GPUs on each node of your cluster
- Keep track of the health of your GPUs
- Run GPU enabled containers in your Kubernetes cluster.

This repository contains Volcano's official implementation of the [Kubernetes device plugin](https://github.com/kubernetes/design-proposals-archive/blob/main/resource-management/device-plugin.md).

## Prerequisites

The list of prerequisites for running the Volcano device plugin is described below:
* NVIDIA drivers ~= 384.81
* nvidia-docker version > 2.0 (see how to [install](https://github.com/NVIDIA/nvidia-docker) and it's [prerequisites](https://github.com/nvidia/nvidia-docker/wiki/Installation-\(version-2.0\)#prerequisites))
* docker configured with nvidia as the [default runtime](https://github.com/NVIDIA/nvidia-docker/wiki/Advanced-topics#default-runtime).
* Kubernetes version >= 1.10

## Quick Start

### Preparing your GPU Nodes

The following steps need to be executed on all your GPU nodes.
This README assumes that the NVIDIA drivers and nvidia-docker have been installed.

Note that you need to install the nvidia-docker2 package and not the nvidia-container-toolkit.
This is because the new `--gpus` options hasn't reached kubernetes yet. Example:
```bash
# Add the package repositories
$ distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
$ curl -s -L https://nvidia.github.io/nvidia-docker/gpgkey | sudo apt-key add -
$ curl -s -L https://nvidia.github.io/nvidia-docker/$distribution/nvidia-docker.list | sudo tee /etc/apt/sources.list.d/nvidia-docker.list

$ sudo apt-get update && sudo apt-get install -y nvidia-docker2
$ sudo systemctl restart docker
```

You will need to enable the nvidia runtime as your default runtime on your node.
We will be editing the docker daemon config file which is usually present at `/etc/docker/daemon.json`:
```json
{
    "default-runtime": "nvidia",
    "runtimes": {
        "nvidia": {
            "path": "/usr/bin/nvidia-container-runtime",
            "runtimeArgs": []
        }
    }
}
```
> *if `runtimes` is not already present, head to the install page of [nvidia-docker](https://github.com/NVIDIA/nvidia-docker)*

### Enabling GPU Support in Kubernetes

Once you have enabled this option on *all* the GPU nodes you wish to use,
you can then enable GPU support in your cluster by deploying the following Daemonset:

```shell
$ kubectl create -f volcano-device-plugin.yml
```

**Note** that volcano device plugin can be configured. For example, it can specify gpu strategy by adding in the yaml file ''args: ["--gpu-strategy=number"]'' under ''image: volcanosh/volcano-device-plugin''. More configuration can be found at [volcano device plugin configuration](https://github.com/volcano-sh/devices/blob/master/doc/config.md).

### Running GPU Sharing Jobs (Without memory isolation)

NVIDIA GPUs can now be shared via container level resource requirements using the resource name volcano.sh/gpu-memory:

The node resource capability and allocatable metadata will show volcano.sh/gpu-number, but user **can not** specify this resource name at the container level. This is because the device-plugin patches volcano.sh/gpu-number to show the total number of gpus, which is only used for volcano scheduler to calculate the memory for each gpu. GPU number in this mode is not registered in kubelet and does not have health-check on it.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod1
spec:
  schedulerName: volcano
  containers:
    - name: cuda-container
      image: nvidia/cuda:9.0-devel
      resources:
        limits:
          volcano.sh/gpu-memory: 1024 # requesting 1024MB GPU memory
---
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod2
spec:
  schedulerName: volcano
  containers:
    - name: cuda-container
      image: nvidia/cuda:9.0-devel
      resources:
        limits:
          volcano.sh/gpu-memory: 1024 # requesting 1024MB GPU memory
```

> **WARNING:** *if you don't request GPUs when using the device plugin with NVIDIA images all
> the GPUs on the machine will be exposed inside your container.*

### Running GPU Number Jobs (Without number isolation)

NVIDIA GPUs can now be requested via container level resource requirements using the resource name volcano.sh/gpu-number:

```shell script
$ cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod1
spec:
  containers:
    - name: cuda-container
      image: nvidia/cuda:9.0-devel
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          volcano.sh/gpu-number: 1 # requesting 1 gpu cards
EOF
```

## Docs

Please note that:
- the device plugin feature is beta as of Kubernetes v1.11.
- the Volcano device plugin is alpha and is missing
    - More comprehensive GPU health checking features
    - GPU cleanup features
    - GPU hard isolation
    - ...

The next sections are focused on building the device plugin and running it.

### With Docker

#### Build
```shell
$ make ubuntu20.04.
```

#### Run locally
```shell
$ docker run --security-opt=no-new-privileges --cap-drop=ALL --network=none -it -v /var/lib/kubelet/device-plugins:/var/lib/kubelet/device-plugins nvidia/k8s-device-plugin:{version}
```

#### Deploy as DaemonSet:
```shell
$ kubectl create -f nvidia-device-plugin.yml
```

# Issues and Contributing
[Checkout the Contributing document!](CONTRIBUTING.md)

* You can report a bug by [filing a new issue](https://github.com/volcano-sh/devices)
* You can contribute by opening a [pull request](https://help.github.com/articles/using-pull-requests/)

## Versioning

The version exactly matches with [Volcano](https://github.com/volcano-sh/volcano).

## Upgrading Kubernetes with the device plugin

Upgrading Kubernetes when you have a device plugin deployed doesn't require you to do any,
particular changes to your workflow.
The API is versioned and is pretty stable (though it is not guaranteed to be non breaking),
upgrading kubernetes won't require you to deploy a different version of the device plugin and you will
see GPUs re-registering themselves after you node comes back online.


Upgrading the device plugin is a more complex task. It is recommended to drain GPU tasks as
we cannot guarantee that GPU tasks will survive a rolling upgrade.
However we make best efforts to preserve GPU tasks during an upgrade.
