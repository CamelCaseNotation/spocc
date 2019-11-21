/*

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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	paulv1alpha1 "github.com/camelcasenotation/spocc/api/v1alpha1"
)

var spoccResourceLabel = map[string]string{
	"spocc": "blart",
}

// SpoccReconciler reconciles a Spocc object
type SpoccReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=paul.blart,resources=spoccs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=paul.blart,resources=spoccs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=limitranges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete

// Reconcile creates a default LimitRange and ResourceQuota in Namespaces with label "app=decco" which
// doesn't have one created by this controller
func (r *SpoccReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("spocc", req.NamespacedName)

	var namespace corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Namespace")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, err
	}
	if val, ok := namespace.Labels["app"]; !ok {
		if val != "decco" {
			// This namespace does not have a label with a key which equals "app" and has value "decco"
			return ctrl.Result{}, nil
		}
	}

	// Check if a LimitRange exists in this Namespace named "default" (which we created)
	limitRange := corev1.LimitRange{}
	defaultLimitRange := client.ObjectKey{Namespace: namespace.Name, Name: "default"}
	if err := r.Get(ctx, defaultLimitRange, &limitRange); err != nil {
		// Could not find the LimitRange, so create it
		if apierrors.IsNotFound(err) {
			if err := r.createDefaultLimitRange(ctx, namespace); err != nil {
				// Dunno why we couldn't create the LimitRange object in this Namespace, but try again after 30 seconds
				return ctrl.Result{
					Requeue:      true,
					RequeueAfter: 30 * time.Second,
				}, err
			}
		}
	}

	// Check if a ResourceQuota exists in this Namespace named "default" (which we created)
	resourceQuota := corev1.ResourceQuota{}
	defaultResourceQuota := client.ObjectKey{Namespace: namespace.Name, Name: "default"}
	if err := r.Get(ctx, defaultResourceQuota, &resourceQuota); err != nil {
		// Could not find the ResourceQuota, so create it
		if apierrors.IsNotFound(err) {
			if err := r.createDefaultResourceQuota(ctx, namespace); err != nil {
				// Dunno why we couldn't create the ResourceQuota object in this Namespace, but try again after 30 seconds
				return ctrl.Result{
					Requeue:      true,
					RequeueAfter: 30 * time.Second,
				}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *SpoccReconciler) createDefaultLimitRange(ctx context.Context, namespace corev1.Namespace) error {
	defaultLimitRange := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: namespace.Name,
			Labels:    spoccResourceLabel,
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Default: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"),
					},
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("50m"),
					},
				},
			},
		},
	}
	if err := r.Client.Create(ctx, defaultLimitRange); err != nil {
		r.Log.Error(err, fmt.Sprintf("Could not create default LimitRange object in namespace %s", namespace.Name))
		return err
	}
	r.Log.Info(fmt.Sprintf("Created default LimitRange object in Namespace %s", namespace.Name))
	return nil
}

func (r *SpoccReconciler) createDefaultResourceQuota(ctx context.Context, namespace corev1.Namespace) error {
	defaultResourceQuota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: namespace.Name,
			Labels:    spoccResourceLabel,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU:    resource.MustParse("8"),
				corev1.ResourceLimitsMemory: resource.MustParse("12Gi"),
			},
		},
	}
	if err := r.Client.Create(ctx, defaultResourceQuota); err != nil {
		r.Log.Error(err, fmt.Sprintf("Could not create default ResourceQuota object in namespace %s", namespace.Name))
		return err
	}
	r.Log.Info(fmt.Sprintf("Created default ResourceQuota object in Namespace %s", namespace.Name))
	return nil
}

// Reference:
// https://github.com/kubernetes-sigs/cluster-api/blob/master/controllers/machineset_controller.go#L476

// SetupWithManager makes this controller only worry about events on Namespaces
func (r *SpoccReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// https://github.com/kubernetes-sigs/cluster-api/blob/master/controllers/machineset_controller.go#L76
	// https://github.com/kubernetes-sigs/cluster-api/blob/master/controllers/machineset_controller.go#L476
	// Maybe use the above code to enable re-creating deleted LimitRanges in a Namespace

	// NOTE: only the last "For()" is registered
	return ctrl.NewControllerManagedBy(mgr).
		// TODO: Remove Spocc API resource since we only created it for kubebuilder scaffolding to be happy
		For(&paulv1alpha1.Spocc{}).
		For(&corev1.Namespace{}).
		Owns(&corev1.LimitRange{}).
		Complete(r)
}
