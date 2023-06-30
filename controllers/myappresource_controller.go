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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

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

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: myAppResource.Name, Namespace: myAppResource.Namespace}, found)

	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		dep, err := r.deploymentForMyAppResource(myAppResource)
		if err != nil {
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

		log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, dep); err != nil {
			log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *MyAppResourceReconciler) doFinalizerOperationsForMyAppResource(cr *myv1alpha1.MyAppResource) {
	r.Recorder.Event(cr, "Warning", "Deleting", fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s", cr.Name, cr.Namespace))
}

// deploymentForMyAppResource returns a MyAppResource Deployment object
func (r *MyAppResourceReconciler) deploymentForMyAppResource(myAppResource *myv1alpha1.MyAppResource) (*appsv1.Deployment, error) {
	ls := labelsForMyAppResource(myAppResource.Name, myAppResource.Spec.Image.Tag)
	replicas := myAppResource.Spec.ReplicaCount

	// Get the Operand image
	image, err := imageForMyAppResource(myAppResource)
	if err != nil {
		return nil, err
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      myAppResource.Name,
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
						Command: []string{"./podinfo", "--port=9898", "--port-metrics=9797", "--grpc-port=9999", "--grpc-service-name=podinfo", "--level=info", "--random-delay=false", "--random-error=false"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: myAppResource.Spec.Resources.CpuRequest,
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: myAppResource.Spec.Resources.MemoryLimit,
							},
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

// labelsForMyAppResource returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForMyAppResource(name string, imageTag string) map[string]string {
	return map[string]string{"app.kubernetes.io/name": "MyAppResource",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/version":    imageTag,
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
