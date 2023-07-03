/*
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
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	myv1alpha1 "github.com/trevorackerman/podinfo-operator/api/v1alpha1"
)

// Definitions to manage status conditions
const (
	myAppResourceFinalizer = "my.api.group/finalizer"
	// typeAvailableMyAppResource represents the status of the Deployment reconciliation
	typeAvailableMyAppResource = "Available"
	// typeDegradedMyAppResource represents the status used when the custom resource is deleted and the finalizer operations are must to occur.
	typeDegradedMyAppResource = "Degraded"
)

// MyAppResourceReconciler reconciles a MyAppResource object
type MyAppResourceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=my.api.group,resources=myappresources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=my.api.group,resources=myappresources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=my.api.group,resources=myappresources/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *MyAppResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	myAppResource := &myv1alpha1.MyAppResource{}
	err := r.Get(ctx, req.NamespacedName, myAppResource)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then, it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("MyAppResource resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get MyAppResource")
		return ctrl.Result{}, err
	}

	if myAppResource.Status.Conditions == nil || len(myAppResource.Status.Conditions) == 0 {
		meta.SetStatusCondition(
			&myAppResource.Status.Conditions,
			metav1.Condition{
				Type:    typeAvailableMyAppResource,
				Status:  metav1.ConditionUnknown,
				Reason:  "Reconciling",
				Message: "Starting reconciliation",
			},
		)

		if err = r.Status().Update(ctx, myAppResource); err != nil {
			log.Error(err, "Failed to update MyAppResource status")
			return ctrl.Result{}, err
		}

		if err := r.Get(ctx, req.NamespacedName, myAppResource); err != nil {
			log.Error(err, "Failed to re-fetch myAppResource")
			return ctrl.Result{}, err
		}
	}

	if !controllerutil.ContainsFinalizer(myAppResource, myAppResourceFinalizer) {
		log.Info("Adding Finalizer for MyAppResource")
		if ok := controllerutil.AddFinalizer(myAppResource, myAppResourceFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, myAppResource); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	isMyAppResourceMarkedToBeDeleted := myAppResource.GetDeletionTimestamp() != nil
	if isMyAppResourceMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(myAppResource, myAppResourceFinalizer) {
			log.Info("Performing Finalizer Operations for MyAppResource before delete CR")

			// Let's add here an status "Downgrade" to define that this resource begin its process to be terminated.
			meta.SetStatusCondition(&myAppResource.Status.Conditions, metav1.Condition{Type: typeDegradedMyAppResource,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", myAppResource.Name)})

			if err := r.Status().Update(ctx, myAppResource); err != nil {
				log.Error(err, "Failed to update MyAppResource status")
				return ctrl.Result{}, err
			}

			r.doFinalizerOperationsForMyAppResource(myAppResource)

			// TODO(user): If you add operations to the doFinalizerOperationsForMyAppResource method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			if err := r.Get(ctx, req.NamespacedName, myAppResource); err != nil {
				log.Error(err, "Failed to re-fetch myAppResource")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(
				&myAppResource.Status.Conditions,
				metav1.Condition{
					Type:    typeDegradedMyAppResource,
					Status:  metav1.ConditionTrue,
					Reason:  "Finalizing",
					Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", myAppResource.Name),
				},
			)

			if err := r.Status().Update(ctx, myAppResource); err != nil {
				log.Error(err, "Failed to update MyAppResource status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for MyAppResource")
			if ok := controllerutil.RemoveFinalizer(myAppResource, myAppResourceFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for MyAppResource")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, myAppResource); err != nil {
				log.Error(err, "Failed to remove finalizer for MyAppResource")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// TODO - refactor duplicated code for spinning up deployment...
	// Check if the deployment already exists, if not create a new one
	podinfoName := fmt.Sprintf("%s-podinfo", myAppResource.Name)
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: podinfoName, Namespace: myAppResource.Namespace}, found)

	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		dep, err := r.definePodinfoDeployment(podinfoName, myAppResource)
		if err != nil {
			return r.HandleDeploymentDefinitionError(ctx, myAppResource, err, log)
		}

		log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, dep); err != nil {
			log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}

	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	} else {
		log.Info("Checking if found deployment matches desired settings")
		// We've found the deployment, does it need to be updated?
		desired, err := r.definePodinfoDeployment(podinfoName, myAppResource)
		if err != nil {
			return r.HandleDeploymentDefinitionError(ctx, myAppResource, err, log)
		}

		if !podinfoDeploymentsEqual(found, desired, log) {
			log.Info("Current podinfo deployment does not match desired state")
			if err = r.Update(ctx, desired); err != nil {
				log.Error(err, "Failed updating Podinfo Deployment", "Namespace", found.Namespace, "Name", found.Name)
				if err := r.Get(ctx, req.NamespacedName, myAppResource); err != nil {
					log.Error(err, "Failed to re-fetch MyAppResource")
					return ctrl.Result{}, err
				}

				// The following implementation will update the status
				meta.SetStatusCondition(&myAppResource.Status.Conditions, metav1.Condition{Type: typeAvailableMyAppResource,
					Status: metav1.ConditionFalse, Reason: "Resizing",
					Message: fmt.Sprintf("Failed to update the size for the custom resource (%s): (%s)", myAppResource.Name, err)})

				if err := r.Status().Update(ctx, myAppResource); err != nil {
					log.Error(err, "Failed to update MyAppResource status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, err
			}
		}
	}

	redisName := fmt.Sprintf("%s-redis", myAppResource.Name)
	if myAppResource.Spec.Redis.Enabled {
		foundConfig := &corev1.ConfigMap{}
		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundConfig)
		if err != nil && apierrors.IsNotFound(err) {
			// Shamelessly copied from https://github.com/stefanprodan/podinfo/blob/master/charts/podinfo/templates/redis/config.yaml
			redisConfig := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      redisName,
					Namespace: myAppResource.Namespace,
				},
				Data: map[string]string{
					"redis.conf": `maxmemory 64mb
maxmemory-policy allkeys-lru
save ""
appendonly no`,
				},
			}

			if err := ctrl.SetControllerReference(myAppResource, redisConfig, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner reference Redis ConfigMap", "ConfigMap.Name", redisConfig.Name)
				return ctrl.Result{}, err
			}

			log.Info("Creating Redis ConfigMap", "ConfigMap.Name", redisConfig.Name)
			if err = r.Create(ctx, redisConfig); err != nil {
				log.Error(err, "Failed to create new Redis ConfigMap", "ConfigMap.Name", redisConfig.Name)
				return ctrl.Result{}, err
			}
		} else if err != nil {
			log.Error(err, "Failed to get ConfigMap")
			// Let's return the error for the reconciliation be re-trigged again
			return ctrl.Result{}, err
		}

		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, found)

		if err != nil && apierrors.IsNotFound(err) {
			// Define a new deployment
			dep, err := r.redisDeployment(redisName, myAppResource)
			if err != nil {
				log.Error(err, "Failed to define new Redis Deployment resource for MyAppResource")

				// The following implementation will update the status
				meta.SetStatusCondition(
					&myAppResource.Status.Conditions,
					metav1.Condition{
						Type:    typeAvailableMyAppResource,
						Status:  metav1.ConditionFalse,
						Reason:  "Reconciling",
						Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", myAppResource.Name, err),
					},
				)

				if err := r.Status().Update(ctx, myAppResource); err != nil {
					log.Error(err, "Failed to update MyAppResource status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, err
			}

			log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			if err = r.Create(ctx, dep); err != nil {
				log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
				return ctrl.Result{}, err
			}

		} else if err != nil {
			log.Error(err, "Failed to get Deployment")
			// Let's return the error for the reconciliation be re-trigged again
			return ctrl.Result{}, err
		}

		foundService := &corev1.Service{}

		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundService)
		if err != nil && apierrors.IsNotFound(err) {
			labels := labelsForMyAppResource(myAppResource.Name)
			labels["app"] = redisName

			// Shamelessly copied from https://github.com/stefanprodan/podinfo/blob/master/charts/podinfo/templates/redis/service.yaml
			redisService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      redisName,
					Namespace: myAppResource.Namespace,
					Labels:    labels,
				},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Selector: labels,
					Ports: []corev1.ServicePort{
						{
							Name:       "redis",
							Port:       6379,
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.IntOrString{IntVal: 6379},
						},
					},
				},
			}

			if err := ctrl.SetControllerReference(myAppResource, redisService, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner reference Redis Service", "Service.Name", redisService.Name)
				return ctrl.Result{}, err
			}

			log.Info("Creating Redis Service", "Service.Name", redisService.Name)
			if err = r.Create(ctx, redisService); err != nil {
				log.Error(err, "Failed to create new Redis Service", "Service.Name", redisService.Name)
				return ctrl.Result{}, err
			}
		} else if err != nil {
			log.Error(err, "Failed to get Service")
			// Let's return the error for the reconciliation be re-trigged again
			return ctrl.Result{}, err
		}
	} else {
		foundService := &corev1.Service{}
		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundService)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		} else if err == nil {
			r.Delete(ctx, foundService)
		}

		foundDeployment := &appsv1.Deployment{}
		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundDeployment)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		} else if err == nil {
			r.Delete(ctx, foundDeployment)
		}

		foundConfig := &corev1.ConfigMap{}
		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundConfig)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		} else if err == nil {
			r.Delete(ctx, foundConfig)
		}
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *MyAppResourceReconciler) doFinalizerOperationsForMyAppResource(cr *myv1alpha1.MyAppResource) {
	if r.Recorder == nil {
		return
	}

	r.Recorder.Event(cr, "Warning", "Deleting", fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s", cr.Name, cr.Namespace))
}

func podinfoDeploymentsEqual(a *appsv1.Deployment, b *appsv1.Deployment, log logr.Logger) bool {
	if *a.Spec.Replicas != *b.Spec.Replicas {
		log.Info("mismatched replicas", "a", a.Spec.Replicas, "b", b.Spec.Replicas)
		return false
	}

	// Leveraging the fact we only create a single container in the pod
	aContainer := a.Spec.Template.Spec.Containers[0]
	bContainer := b.Spec.Template.Spec.Containers[0]

	if aContainer.Image != bContainer.Image {
		log.Info("Mismatched images", "a", aContainer.Image, "b", bContainer.Image)
		return false
	}

	return envsEqual(aContainer.Env, bContainer.Env, log) &&
		resourcesEqual(aContainer.Resources, bContainer.Resources, log) &&
		commandsEqual(aContainer.Command, bContainer.Command, log)
}

func envsEqual(a []corev1.EnvVar, b []corev1.EnvVar, log logr.Logger) bool {
	if len(a) != len(b) {
		log.Info("different length envs", "a", len(a), "b", len(b))
		return false
	}

	m := make(map[string]string)

	for _, item := range a {
		m[item.Name] = item.Value
	}

	for _, item := range b {
		val, ok := m[item.Name]
		if ok && val == item.Value {
			delete(m, item.Name)
		} else if !ok {
			m[item.Name] = item.Value
		}
	}

	if len(m) != 0 {
		for key, val := range m {
			log.Info("Found mismatched env var", "key", key, "value", val)
		}
		return false
	}
	return true
}

func resourcesEqual(a corev1.ResourceRequirements, b corev1.ResourceRequirements, log logr.Logger) bool {
	if !a.Limits.Memory().Equal(*b.Limits.Memory()) {
		log.Info("Mismatched memory limits", "a", a.Limits.Memory(), "b", b.Limits.Memory())
		return false
	}

	if !a.Requests.Cpu().Equal(*b.Requests.Cpu()) {
		log.Info("Mismatched CPU requests", "a", a.Requests.Cpu(), "b", b.Requests.Cpu())
		return false
	}

	return true
}

func commandsEqual(a []string, b []string, log logr.Logger) bool {
	if len(a) != len(b) {
		log.Info("Mismatched commands (len)", "a", len(a), "b", len(b))
		return false
	}

	ok := true
	for i, item := range a {
		if b[i] != item {
			ok = false
			log.Info("Found mismatched string in command", "index", i, "a", a[i], "b", b[i])
		}
	}

	return ok
}

// definePodinfoDeployment returns a MyAppResource Deployment object
func (r *MyAppResourceReconciler) definePodinfoDeployment(podinfoName string, myAppResource *myv1alpha1.MyAppResource) (*appsv1.Deployment, error) {
	ls := labelsForMyAppResource(podinfoName)
	replicas := myAppResource.Spec.ReplicaCount

	// Get the Operand image
	image, err := imageForMyAppResource(myAppResource)
	if err != nil {
		return nil, err
	}

	command := []string{"./podinfo", "--port=9898", "--port-metrics=9797", "--grpc-port=9999", "--grpc-service-name=podinfo", "--level=info", "--random-delay=false", "--random-error=false"}

	if myAppResource.Spec.Redis.Enabled {
		command = append(command, fmt.Sprintf("--cache-server=tcp://%s-redis:6379", myAppResource.Name))
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podinfoName,
			Namespace: myAppResource.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					// Shamelessly copied from https://github.com/stefanprodan/podinfo/blob/master/kustomize/deployment.yaml
					Containers: []corev1.Container{{
						Image:           image,
						Name:            "podinfo",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &[]bool{true}[0],
							RunAsUser:                &[]int64{1001}[0],
							AllowPrivilegeEscalation: &[]bool{false}[0],
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 9898,
								Name:          "http",
							},
							{
								ContainerPort: 9797,
								Name:          "http-metrics",
							},
							{
								ContainerPort: 9999,
								Name:          "grpc",
							},
						},
						Command: command,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: myAppResource.Spec.Resources.CpuRequest,
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: myAppResource.Spec.Resources.MemoryLimit,
							},
						},
						Env: []corev1.EnvVar{
							{Name: "PODINFO_UI_COLOR", Value: myAppResource.Spec.UI.Color},
							{Name: "PODINFO_UI_MESSAGE", Value: myAppResource.Spec.UI.Message},
						},
					}},
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(myAppResource, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

func (r *MyAppResourceReconciler) redisDeployment(redisName string, myAppResource *myv1alpha1.MyAppResource) (*appsv1.Deployment, error) {
	ls := labelsForMyAppResource(myAppResource.Name)
	ls["app"] = redisName

	// Shamelessly copied from https://github.com/stefanprodan/podinfo/blob/master/deploy/bases/cache/deployment.yaml
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      redisName,
			Namespace: myAppResource.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Image:           "redis:7.0.7",
						Name:            "redis",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"redis-server", "/redis-master/redis.conf"},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &[]bool{true}[0],
							RunAsUser:                &[]int64{1001}[0],
							AllowPrivilegeEscalation: &[]bool{false}[0],
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 6379,
								Name:          "redis",
							},
						},
						LivenessProbe: &corev1.Probe{
							InitialDelaySeconds: 5,
							TimeoutSeconds:      5,
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.IntOrString{
										IntVal: 6379,
									},
								},
							},
						},
						ReadinessProbe: &corev1.Probe{
							InitialDelaySeconds: 5,
							TimeoutSeconds:      5,
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{
									Command: []string{"redis-cli", "ping"},
								},
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("32Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1000m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: "/var/lib/redis",
								Name:      "data",
							},
							{
								MountPath: "/redis-master",
								Name:      "config",
							},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: redisName,
									},
									Items: []corev1.KeyToPath{
										{
											Key:  "redis.conf",
											Path: "redis.conf",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(myAppResource, dep, r.Scheme); err != nil {
		return nil, err
	}

	return dep, nil
}

// labelsForMyAppResource returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForMyAppResource(name string) map[string]string {
	return map[string]string{"app.kubernetes.io/name": "MyAppResource",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/part-of":    "podinfo-operator",
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

func imageForMyAppResource(myAppResource *myv1alpha1.MyAppResource) (string, error) {
	return fmt.Sprintf("%s:%s", myAppResource.Spec.Image.Repository, myAppResource.Spec.Image.Tag), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MyAppResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&myv1alpha1.MyAppResource{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

func (r *MyAppResourceReconciler) HandleDeploymentDefinitionError(ctx context.Context, myAppResource *myv1alpha1.MyAppResource, err error, log logr.Logger) (ctrl.Result, error) {
	log.Error(err, "Failed to define new Deployment resource for MyAppResource")

	// The following implementation will update the status
	meta.SetStatusCondition(
		&myAppResource.Status.Conditions,
		metav1.Condition{
			Type:    typeAvailableMyAppResource,
			Status:  metav1.ConditionFalse,
			Reason:  "Reconciling",
			Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", myAppResource.Name, err),
		},
	)

	if err := r.Status().Update(ctx, myAppResource); err != nil {
		log.Error(err, "Failed to update MyAppResource status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, err
}
