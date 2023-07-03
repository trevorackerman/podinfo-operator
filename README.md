# podinfo
Launches Podinfo deployment with optional Redis Service as a single custom resource

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster
1. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/my_v1alpha1_myappresource.yaml
```

2. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push
```
  1. With Minikube
```sh
make docker-build
```

3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller from the cluster:

```sh
make undeploy
```

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

# Development Notes

This project was bootstrapped with the command
```sh
operator-sdk init --domain api.group --project-name=podinfo --repo github.com/trevorackerman/podinfo-operator
```
See https://sdk.operatorframework.io/docs/building-operators/golang/tutorial/#create-a-new-project

Creating the MyAppResource API
```sh
operator-sdk create api --group my --version v1alpha1 --kind MyAppResource --resource --controller
```
## Working with Minikube

Every time you start a new terminal run

```sh
eval $(minikube docker-env)
```

Otherwise running `make docker-build docker-push` will fail to push to the container registry used by minikube to pull your controller image
See https://minikube.sigs.k8s.io/docs/handbook/pushing/#1-pushing-directly-to-the-in-cluster-docker-daemon-docker-env

You can check if you've correctly pushed to minikube via

```sh
minikube image ls
```

### Running tests with minikube

```sh
export KUBEBUILDER_ASSETS=$(pwd)/bin/k8s/1.26.0-linux-amd64
go test ./controllers/... -v -ginkgo.v
```

## Access Podinfo through port forward

See https://kubernetes.io/docs/tasks/access-application-cluster/port-forward-access-application-cluster/ for more details
The http port will be 9898, this can be verified by running the following command and examining the port named "http"
```sh
kubectl port-forward deployments/myappresource-sample-podinfo :9898
```

## Creating MyAppResource custom resources
```sh
kubectl apply -f config/samples/my_v1alpha1_myappresource.yaml   
```

This should create the following resources
1. A Podinfo Deployment with 2 pods
2. A ConfigMap for Redis
3. A Redis deployment with 1 pod
4. A Redis Service

And you should be able to run busybox in another pod to manually test the podinfo deployment if you don't want to do kube port-forward
```sh
kubectl run -i --tty busybox --image=busybox:1.28 -- sh
/ # wget -O- myappresource-sample-podinfo:9898
```

And see something similar to
```
Connecting to myappresource-sample-podinfo:9898 (10.111.249.179:9898)
{
  "hostname": "myappresource-sample-podinfo-7bbb459fdb-wr2ms",
  "version": "6.4.0",
  "revision": "fcf573111bd82600052f99195a67f33d8242bf17",
  "color": "#34577c",
  "logo": "https://raw.githubusercontent.com/stefanprodan/podinfo/gh-pages/cuddle_clap.gif",
  "message": "greetings from podinfo v6.4.0",
  "goos": "linux",
  "goarch": "amd64",
  "runtime": "go1.20.5",
  "num_goroutine": "8",
  "num_cpu": "8"
-                    100% |*******************************************************************************************************************************************************************|   413   0:00:00 ETA
/ #
```

# Further Work
1. Fill out CRD with meaningful status during reconciliation
1. Record Events during reconciliation, e.g. started deployments, created configmaps...
1. Integrate with Grafana for resource monitoring
1. Reduce code duplication