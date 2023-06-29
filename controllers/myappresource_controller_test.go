package controllers

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	myv1alpha1 "github.com/trevorackerman/podinfo-operator/api/v1alpha1"
)

var _ = Describe("MyAppResource controller", func() {
	Context("MyAppResource controller test", func() {
		const MyAppResourceName = "test-resource"

		ctx := context.Background()

		typeNamespaceName := types.NamespacedName{Name: MyAppResourceName, Namespace: "default"}

		It("Successfully Reconciles", func() {
			By("Creating the custom resource for the Kind MyAppResource")
			myAppResource := &myv1alpha1.MyAppResource{}
			err := k8sClient.Get(ctx, typeNamespaceName, myAppResource)

			if err != nil {
				Expect(errors.IsNotFound(err)).To(BeTrue())
				myAppResource := &myv1alpha1.MyAppResource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MyAppResourceName,
						Namespace: "default",
					},
					Spec: myv1alpha1.MyAppResourceSpec{
						ReplicaCount: 3,
					},
				}

				err = k8sClient.Create(ctx, myAppResource)
				Expect(err).To(Not(HaveOccurred()))
			}

			By("Checking if the resource was created")
			Eventually(func() error {
				found := &myv1alpha1.MyAppResource{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the resource")
			myAppResourceReconciler := &MyAppResourceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = myAppResourceReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			By("Checking the Deployment was successfully created")
			Eventually(func() error {
				found := &appsv1.Deployment{}
				err := k8sClient.Get(ctx, typeNamespaceName, found)
				if err != nil {
					return err
				}

				Expect(*found.Spec.Replicas).To(Equal(int32(3)))
				return nil
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking the latest Status Condition")
			Eventually(func() error {
				if myAppResource.Status.Conditions != nil && len(myAppResource.Status.Conditions) > 0 {
					latestStatusCondition := myAppResource.Status.Conditions[len(myAppResource.Status.Conditions)-1]
					expected := metav1.Condition{
						Type:    typeAvailableMyAppResource,
						Status:  metav1.ConditionTrue,
						Reason:  "Reconciling",
						Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", myAppResource.Name, myAppResource.Spec.ReplicaCount),
					}

					if latestStatusCondition != expected {
						return fmt.Errorf("The latest status condition added to the myAppResource instance is not as expected")
					}
				}

				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})