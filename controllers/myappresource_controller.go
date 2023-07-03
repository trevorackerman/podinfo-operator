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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// TODO - break up this function and reduce duplicated code
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

	if err = r.reconcilePodinfoResources(ctx, myAppResource, req, log); err != nil {
		return ctrl.Result{}, err
	}

	if err = r.reconcileRedisResources(ctx, myAppResource, log); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *MyAppResourceReconciler) doFinalizerOperationsForMyAppResource(cr *myv1alpha1.MyAppResource) {
	if r.Recorder == nil {
		return
	}

	r.Recorder.Event(cr, "Warning", "Deleting", fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s", cr.Name, cr.Namespace))
}

// SetupWithManager sets up the controller with the Manager.
func (r *MyAppResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&myv1alpha1.MyAppResource{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
