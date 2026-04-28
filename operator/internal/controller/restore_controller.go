/*
Copyright 2026.

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

package controller

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dumpscriptv1alpha1 "github.com/cloudscript-technology/dumpscript/operator/api/v1alpha1"
)

// RestoreReconciler reconciles a Restore object.
type RestoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=restores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=restores/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile materialises a Restore into a one-shot batch/v1 Job and reflects
// the Job's terminal state (Succeeded / Failed) back to the Restore status.
func (r *RestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var restore dumpscriptv1alpha1.Restore
	if err := r.Get(ctx, req.NamespacedName, &restore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Nothing to do once we reach a terminal phase.
	if restore.Status.Phase == dumpscriptv1alpha1.RestorePhaseSucceeded ||
		restore.Status.Phase == dumpscriptv1alpha1.RestorePhaseFailed {
		return ctrl.Result{}, nil
	}

	desired := buildRestoreJob(&restore)
	if err := controllerutil.SetControllerReference(&restore, desired, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("set controller ref: %w", err)
	}

	current := &batchv1.Job{}
	err := r.Get(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, current)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, desired); err != nil {
			return ctrl.Result{}, fmt.Errorf("create restore job: %w", err)
		}
		log.Info("created restore job", "job", desired.Name)
		now := metav1.Now()
		restore.Status.Phase = dumpscriptv1alpha1.RestorePhasePending
		restore.Status.JobName = desired.Name
		restore.Status.StartedAt = &now
		return ctrl.Result{}, r.Status().Update(ctx, &restore)
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get restore job: %w", err)
	}

	// Reflect Job terminal state back to the Restore status.
	switch {
	case current.Status.Succeeded > 0:
		restore.Status.Phase = dumpscriptv1alpha1.RestorePhaseSucceeded
		restore.Status.CompletedAt = current.Status.CompletionTime
		log.Info("restore succeeded", "job", current.Name)
	case current.Status.Failed > 0:
		restore.Status.Phase = dumpscriptv1alpha1.RestorePhaseFailed
		restore.Status.Message = fmt.Sprintf("job %s failed after %d attempt(s)", current.Name, current.Status.Failed)
		log.Info("restore failed", "job", current.Name)
	case current.Status.Active > 0:
		restore.Status.Phase = dumpscriptv1alpha1.RestorePhaseRunning
	}
	return ctrl.Result{}, r.Status().Update(ctx, &restore)
}

// SetupWithManager registers the controller and watches owned Jobs so that
// Job completion events re-trigger reconciliation.
func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dumpscriptv1alpha1.Restore{}).
		Owns(&batchv1.Job{}).
		Named("restore").
		Complete(r)
}
